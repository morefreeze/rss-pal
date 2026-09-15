package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrVerificationRequired = errors.New("verification required")
var ErrVerificationUnavailable = errors.New("verification unavailable")

type RegistrationVerifier interface {
	Verify(context.Context, string, string) error
}

type TurnstileVerifier struct {
	secret    string
	hostnames map[string]bool
	client    *http.Client
	endpoint  string
}

func NewTurnstileVerifier(secret string, hostnames []string) *TurnstileVerifier {
	hosts := map[string]bool{}
	for _, host := range hostnames {
		if host = strings.TrimSpace(host); host != "" {
			hosts[host] = true
		}
	}
	return &TurnstileVerifier{secret: secret, hostnames: hosts, endpoint: "https://challenges.cloudflare.com/turnstile/v0/siteverify", client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (v *TurnstileVerifier) Verify(ctx context.Context, token, ip string) error {
	if v.secret == "" || len(v.hostnames) == 0 {
		return ErrVerificationUnavailable
	}
	if len(token) == 0 || len(token) > 2048 {
		return ErrVerificationRequired
	}
	form := url.Values{"secret": {v.secret}, "response": {token}, "remoteip": {ip}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrVerificationUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := v.client.Do(req)
	if err != nil {
		return ErrVerificationUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ErrVerificationUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		return ErrVerificationUnavailable
	}
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if json.Unmarshal(body, &result) != nil {
		return ErrVerificationUnavailable
	}
	if !result.Success || !v.hostnames[result.Hostname] || result.Action != "signup" {
		return ErrVerificationRequired
	}
	return nil
}
