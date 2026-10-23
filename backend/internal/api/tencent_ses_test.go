package api

import (
	"context"
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/config"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func sesTestConfig() config.AuthConfig {
	return config.AuthConfig{EmailProvider: "tencent-ses", SESSecretID: "ses-id", SESSecretKey: "ses-key", SESRegion: "ap-hongkong", SESTemplateID: "227021", SESFrom: "RSS Pal <noreply@rss.morefreeze.top>", SESReplyTo: "morefreeze@gmail.com"}
}
func TestSESContract(t *testing.T) {
	sender := newSESEmailSender(sesTestConfig())
	sender.client.WithHttpTransport(captchaTransport{base: captchaRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.Scheme != "https" || r.URL.Host != "ses.tencentcloudapi.com" || r.Header.Get("X-TC-Action") != "SendEmail" || r.Header.Get("X-TC-Version") != "2020-10-02" || r.Header.Get("X-TC-Region") != "ap-hongkong" || !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=ses-id/") {
			t.Error("incorrect signed SES request")
		}
		if body["FromEmailAddress"] != "RSS Pal <noreply@rss.morefreeze.top>" || body["ReplyToAddresses"] != "morefreeze@gmail.com" || body["TriggerType"] != float64(1) {
			t.Error("incorrect transactional sender")
		}
		recipients := body["Destination"].([]any)
		if len(recipients) != 1 || recipients[0] != "reader@example.com" {
			t.Error("incorrect destination")
		}
		template := body["Template"].(map[string]any)
		var vars map[string]string
		json.Unmarshal([]byte(template["TemplateData"].(string)), &vars)
		if template["TemplateID"] != float64(227021) || vars["code"] != "123456" {
			t.Error("incorrect template")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Error("missing deadline")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"Response":{"MessageId":"accepted","RequestId":"req"}}`))}, nil
	})})
	if err := sender.Send(context.Background(), "reader@example.com", "123456"); err != nil {
		t.Fatal(err)
	}
}
func TestSESFailClosed(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{200, `{}`}, {200, `{"Response":{}}`}, {200, `{"Response":{"Error":{"Code":"AuthFailure","Message":"secret diagnostic"}}}`}, {200, `invalid`}, {302, `{}`}, {500, `{}`}} {
		sender := newSESEmailSender(sesTestConfig())
		calls := 0
		sender.client.WithHttpTransport(captchaTransport{base: captchaRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://example.com"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})})
		if err := sender.Send(context.Background(), "reader@example.com", "123456"); err == nil || strings.Contains(err.Error(), "secret diagnostic") {
			t.Error("must fail with sanitized error")
		}
		if calls != 1 {
			t.Errorf("unexpected retry count %d", calls)
		}
	}
}
func TestSESConfiguration(t *testing.T) {
	for _, field := range []string{"valid", "key", "id", "region", "template", "from", "reply"} {
		cfg := sesTestConfig()
		switch field {
		case "key":
			cfg.SESSecretKey = ""
		case "id":
			cfg.SESSecretID = ""
		case "region":
			cfg.SESRegion = ""
		case "template":
			cfg.SESTemplateID = "0"
		case "from":
			cfg.SESFrom = "bad"
		case "reply":
			cfg.SESReplyTo = "bad"
		}
		s := newSESEmailSender(cfg)
		if s.ready != (field == "valid") {
			t.Errorf("unexpected readiness for %s", field)
		}
	}
}

func TestSESReadinessAndSelection(t *testing.T) {
	cfg := &config.Config{Auth: sesTestConfig()}
	cfg.Auth.PublicOrigin = "https://rss.morefreeze.top"
	cfg.Auth.OTPSecret = strings.Repeat("k", 32)
	cfg.Auth.TurnstileSiteKey = "site"
	cfg.Auth.TurnstileSecret = "secret"
	cfg.Auth.TurnstileHostnames = []string{"rss.morefreeze.top"}
	h := NewAccountAuthHandler(NewAuthHandler(cfg, nil, nil), nil)
	if !h.emailReady() || h.sesSender == nil {
		t.Fatal("SES not selected or unavailable")
	}
	cfg.Auth.EmailProvider = "unknown"
	cfg.Auth.SMTPHost = "smtp.example.com"
	cfg.Auth.SMTPFrom = "sender@example.com"
	if h.emailReady() {
		t.Fatal("unknown provider silently fell back to SMTP")
	}
}
func TestSESCancellation(t *testing.T) {
	s := newSESEmailSender(sesTestConfig())
	s.client.WithHttpTransport(captchaRoundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Send(ctx, "reader@example.com", "123456"); err == nil {
		t.Fatal("canceled send succeeded")
	}
}
