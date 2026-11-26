package mobilepush

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func TestValidateEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"https://fcm.googleapis.com/fcm/send/token",
		"https://web.push.apple.com/Q-token",
		"https://updates.push.services.mozilla.com/wpush/v2/token",
	} {
		if err := ValidateEndpoint(endpoint); err != nil {
			t.Errorf("rejected provider endpoint %q: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"http://fcm.googleapis.com/fcm/send/token", "https://127.0.0.1/private",
		"https://fcm.googleapis.com:444/path", "https://web.push.apple.com.attacker.example/path",
		"https://user@fcm.googleapis.com/path", "https://fcm.googleapis.com/path#fragment",
	} {
		if err := ValidateEndpoint(endpoint); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
}

func TestPayloadUsesTotalUnread(t *testing.T) {
	body, err := payload(2, 20)
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		Badge        int `json:"app_badge"`
		Notification struct {
			Body     string `json:"body"`
			Navigate string `json:"navigate"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatal(err)
	}
	if message.Badge != 20 || !strings.Contains(message.Notification.Body, "新增 2 篇") ||
		!strings.Contains(message.Notification.Body, "共 20 篇未读") || message.Notification.Navigate != "/articles" {
		t.Fatalf("unexpected notification: %s", body)
	}
}

type pushTransport func(*http.Request) (*http.Response, error)

func (roundTrip pushTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return roundTrip(req)
}

func TestChromeUsesConfiguredEgressAndAppleUsesDirect(t *testing.T) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	testClient := func(wantHost string) *http.Client {
		return &http.Client{Transport: pushTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != wantHost || req.Header.Get("Content-Encoding") != "aes128gcm" {
				t.Fatalf("unexpected dispatch host=%q encoding=%q", req.URL.Host, req.Header.Get("Content-Encoding"))
			}
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
	}
	dispatch := sender{
		keys:   Keys{Private: private, Public: public},
		direct: testClient("web.push.apple.com"),
		fcm:    testClient("fcm.googleapis.com"),
	}
	for _, host := range []string{"fcm.googleapis.com", "web.push.apple.com"} {
		sub := webpush.Subscription{
			Endpoint: "https://" + host + "/sample-endpoint",
			Keys:     webpush.Keys{Auth: encode(make([]byte, 16)), P256dh: encode(recipient.PublicKey().Bytes())},
		}
		if status, err := dispatch.send(context.Background(), sub, 1, 2); err != nil || status != http.StatusCreated {
			t.Fatalf("%s push status=%d err=%v", host, status, err)
		}
	}
}
