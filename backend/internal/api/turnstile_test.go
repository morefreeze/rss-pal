package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTurnstileVerification(t *testing.T) {
	cases := []struct {
		name, reply string
		status      int
		want        bool
	}{
		{"valid", `{"success":true,"hostname":"rss.morefreeze.top","action":"signup"}`, 200, true},
		{"replay", `{"success":false,"error-codes":["timeout-or-duplicate"]}`, 200, false},
		{"wrong host", `{"success":true,"hostname":"localhost","action":"signup"}`, 200, false},
		{"wrong action", `{"success":true,"hostname":"rss.morefreeze.top","action":"login"}`, 200, false},
		{"missing action", `{"success":true,"hostname":"rss.morefreeze.top"}`, 200, false},
		{"malformed", `not json`, 200, false},
		{"outage", `{}`, 503, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("secret") != "test-secret" || r.Form.Get("response") != "proof" || r.Form.Get("remoteip") != "192.0.2.1" {
					t.Error("verification payload mismatch")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.reply))
			}))
			defer server.Close()
			v := NewTurnstileVerifier("test-secret", []string{"rss.morefreeze.top"})
			v.endpoint = server.URL
			v.client = server.Client()
			err := v.Verify(context.Background(), "proof", "192.0.2.1")
			if (err == nil) != tc.want {
				t.Fatalf("verify returned %v", err)
			}
		})
	}
}
func TestTurnstileRejectsMissingConfigurationAndProof(t *testing.T) {
	for _, token := range []string{"", strings.Repeat("x", 2049)} {
		if err := NewTurnstileVerifier("secret", []string{"rss.morefreeze.top"}).Verify(t.Context(), token, ""); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	for _, v := range []*TurnstileVerifier{NewTurnstileVerifier("", []string{"rss.morefreeze.top"}), NewTurnstileVerifier("secret", nil)} {
		if v.Verify(t.Context(), "proof", "") == nil {
			t.Fatal("missing config allowed")
		}
	}
}
