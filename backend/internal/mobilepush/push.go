package mobilepush

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/bytedance/rss-pal/internal/httpx"
)

const fcmHost = "fcm.googleapis.com"

func ValidateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.User != nil ||
		u.Host != u.Hostname() || u.Path == "" || u.Path == "/" || strings.Contains(raw, "#") {
		return errors.New("invalid push endpoint")
	}
	switch host := u.Hostname(); {
	case host == fcmHost, host == "updates.push.services.mozilla.com",
		host == "push.apple.com", strings.HasSuffix(host, ".push.apple.com"):
		return nil
	}
	return errors.New("unsupported push provider")
}

func ValidSubscription(s webpush.Subscription) bool {
	if ValidateEndpoint(s.Endpoint) != nil || len(s.Keys.Auth) != 22 || len(s.Keys.P256dh) != 87 {
		return false
	}
	auth, err := base64.RawURLEncoding.DecodeString(s.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return false
	}
	public, err := base64.RawURLEncoding.DecodeString(s.Keys.P256dh)
	if err != nil {
		return false
	}
	_, err = ecdh.P256().NewPublicKey(public)
	return err == nil
}

func payload(recent, total int) ([]byte, error) {
	return json.Marshal(map[string]any{
		"app_badge": total,
		"notification": map[string]string{
			"title":    "RSS Pal",
			"body":     fmt.Sprintf("新增 %d 篇，共 %d 篇未读", recent, total),
			"navigate": "/articles",
		},
	})
}

type sender struct {
	keys   Keys
	direct *http.Client
	fcm    *http.Client
}

func newSender(keys Keys) sender {
	direct := httpx.NewDirectClient(10 * time.Second)
	// Tencent's existing OCI proxy reaches Google when its direct route does not.
	fcm := httpx.NewClient(10 * time.Second)
	for _, client := range []*http.Client{direct, fcm} {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return sender{keys: keys, direct: direct, fcm: fcm}
}

func (s sender) send(ctx context.Context, subscription webpush.Subscription, recent, total int) (int, error) {
	if !ValidSubscription(subscription) {
		return http.StatusGone, nil
	}
	message, err := payload(recent, total)
	if err != nil {
		return 0, err
	}
	client := s.direct
	if strings.HasPrefix(subscription.Endpoint, "https://"+fcmHost+"/") {
		client = s.fcm
	}
	response, err := webpush.SendNotificationWithContext(ctx, message, &subscription, &webpush.Options{
		Subscriber:      "https://rss.morefreeze.top",
		VAPIDPublicKey:  s.keys.Public,
		VAPIDPrivateKey: s.keys.Private,
		HTTPClient:      client,
		TTL:             3600,
		Topic:           "rss-pal-unread",
	})
	if err != nil {
		// Transport errors may contain the device's confidential push endpoint.
		return 0, errors.New("push provider request failed")
	}
	_ = response.Body.Close()
	return response.StatusCode, nil
}
