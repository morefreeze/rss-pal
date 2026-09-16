package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type captchaRoundTripFunc func(*http.Request) (*http.Response, error)

func (f captchaRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTencentCaptchaContract(t *testing.T) {
	v := NewTencentCaptchaVerifier(123, "app-secret", "secret-id", "secret-key")
	v.client.WithHttpTransport(captchaTransport{base: captchaRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]any{"CaptchaType": float64(9), "CaptchaAppId": float64(123), "AppSecretKey": "app-secret", "UserIp": "203.0.113.4", "Ticket": "ticket", "Randstr": "random"} {
			if body[key] != want {
				t.Errorf("%s = %v, want %v", key, body[key], want)
			}
		}
		if r.URL.Host != "captcha.tencentcloudapi.com" || r.Header.Get("X-TC-Action") != "DescribeCaptchaResult" || r.Header.Get("X-TC-Version") != "2019-07-22" || !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=secret-id/") {
			t.Error("unexpected API request")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("request must have <=10s deadline")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"Response":{"CaptchaCode":1}}`)), Header: make(http.Header)}, nil
	})})
	if err := v.Verify(context.Background(), `{"ticket":"ticket","randstr":"random"}`, "203.0.113.4"); err != nil {
		t.Fatal(err)
	}
}

func TestTencentCaptchaFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"rejected", `{"Response":{"CaptchaCode":9}}`, 200, ErrVerificationRequired},
		{"missing code", `{"Response":{}}`, 200, ErrVerificationUnavailable},
		{"null code", `{"Response":{"CaptchaCode":null}}`, 200, ErrVerificationUnavailable},
		{"absent response", `{}`, 200, ErrVerificationUnavailable},
		{"api error", `{"Response":{"Error":{"Code":"AuthFailure","Message":"bad credential"}}}`, 200, ErrVerificationUnavailable},
		{"invalid json", `no`, 200, ErrVerificationUnavailable},
		{"http failure", `{"Response":{"CaptchaCode":1}}`, 500, ErrVerificationUnavailable},
		{"redirect", ``, 302, ErrVerificationUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := NewTencentCaptchaVerifier(123, "app-secret", "secret-id", "secret-key")
			calls := 0
			v.client.WithHttpTransport(captchaTransport{base: captchaRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://example.com"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			if err := v.Verify(context.Background(), `{"ticket":"ticket","randstr":"random"}`, "203.0.113.4"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if calls != 1 {
				t.Errorf("made %d requests", calls)
			}
		})
	}
}

func TestTencentCaptchaInvalidProof(t *testing.T) {
	for _, proof := range []string{"", "null", `{}`, `{"ticket":"ticket"}`, `{"ticket":" ","randstr":"random"}`, `{"ticket":"trerror_abc","randstr":"random"}`, strings.Repeat("x", 2049)} {
		v := NewTencentCaptchaVerifier(123, "app-secret", "secret-id", "secret-key")
		v.client.WithHttpTransport(captchaRoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("invalid proof sent to API")
			return nil, errors.New("unexpected")
		}))
		if err := v.Verify(context.Background(), proof, "203.0.113.4"); !errors.Is(err, ErrVerificationRequired) {
			t.Errorf("invalid proof returned %v", err)
		}
	}
	for _, v := range []*TencentCaptchaVerifier{NewTencentCaptchaVerifier(0, "a", "b", "c"), NewTencentCaptchaVerifier(1, "", "b", "c"), NewTencentCaptchaVerifier(1, "a", "", "c"), NewTencentCaptchaVerifier(1, "a", "b", "")} {
		if err := v.Verify(context.Background(), `{"ticket":"ticket","randstr":"random"}`, "203.0.113.4"); !errors.Is(err, ErrVerificationUnavailable) {
			t.Errorf("missing credentials returned %v", err)
		}
	}
}

func TestTencentCaptchaCancellation(t *testing.T) {
	v := NewTencentCaptchaVerifier(123, "app-secret", "secret-id", "secret-key")
	v.client.WithHttpTransport(captchaRoundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := v.Verify(ctx, `{"ticket":"ticket","randstr":"random"}`, "203.0.113.4"); !errors.Is(err, ErrVerificationUnavailable) {
		t.Fatal(err)
	}
}
