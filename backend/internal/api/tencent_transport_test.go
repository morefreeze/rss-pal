package api

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestTencentAuthAPIsBypassOutboundProxy(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-TC-Action") == "SendEmail" {
			w.Write([]byte(`{"Response":{"MessageId":"accepted","RequestId":"mail-test"}}`))
		} else {
			w.Write([]byte(`{"Response":{"CaptchaCode":1,"RequestId":"captcha-test"}}`))
		}
	}))
	defer server.Close()
	previous := http.DefaultTransport
	base := previous.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Local test server only.
	base.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	base.Proxy = func(*http.Request) (*url.URL, error) {
		proxyCalls.Add(1)
		return nil, errors.New("outbound proxy unavailable")
	}
	http.DefaultTransport = base
	t.Cleanup(func() { http.DefaultTransport = previous; base.CloseIdleConnections() })

	verifier := NewTencentCaptchaVerifier(123, "app-secret", "secret-id", "secret-key")
	if err := verifier.Verify(context.Background(), `{"ticket":"ticket","randstr":"random"}`, "192.0.2.1"); err != nil {
		t.Errorf("captcha depended on the outbound proxy: %v", err)
	}
	sender := newSESEmailSender(sesTestConfig())
	if err := sender.Send(context.Background(), "reader@example.com", "123456"); err != nil {
		t.Errorf("SES depended on the outbound proxy: %v", err)
	}
	if directCalls.Load() != 2 || proxyCalls.Load() != 0 {
		t.Errorf("direct=%d proxy=%d", directCalls.Load(), proxyCalls.Load())
	}
	// The shared transport must remain proxied for unrelated outbound requests.
	if http.DefaultTransport != base || base.Proxy == nil {
		t.Fatal("changed the global outbound network policy")
	}
}
