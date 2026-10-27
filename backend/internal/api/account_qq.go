package api

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// QQ's documented server-side exchange requires GET parameters. All endpoints
// are fixed, redirects are disabled by the callback client, and request URLs or
// response bodies containing credentials must never appear in returned errors.
func qqGetJSON(ctx context.Context, client *http.Client, endpoint string, values url.Values, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return errors.New("invalid QQ request")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("QQ identity request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("QQ identity unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 || json.Unmarshal(data, target) != nil {
		return errors.New("invalid QQ response")
	}
	return nil
}

func qqSubject(ctx context.Context, client *http.Client, o *oauth2.Config, code string) (string, error) {
	var token struct {
		AccessToken string `json:"access_token"`
		Error       *int   `json:"error"`
	}
	err := qqGetJSON(ctx, client, "https://graph.qq.com/oauth2.0/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {o.ClientID}, "client_secret": {o.ClientSecret},
		"code": {code}, "redirect_uri": {o.RedirectURL}, "fmt": {"json"},
	}, &token)
	if err != nil || token.Error != nil || token.AccessToken == "" || len(token.AccessToken) > 8192 {
		return "", errors.New("QQ token exchange failed")
	}
	var identity struct {
		ClientID string `json:"client_id"`
		OpenID   string `json:"openid"`
		Error    *int   `json:"error"`
	}
	if err := qqGetJSON(ctx, client, "https://graph.qq.com/oauth2.0/me", url.Values{"access_token": {token.AccessToken}, "fmt": {"json"}}, &identity); err != nil {
		return "", err
	}
	if identity.Error != nil || identity.ClientID != o.ClientID || identity.OpenID == "" || len(identity.OpenID) > 256 || strings.IndexFunc(identity.OpenID, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		return "", errors.New("invalid QQ identity")
	}
	// OpenID is application-scoped; include the verified app ID in the namespace.
	return o.ClientID + ":" + identity.OpenID, nil
}
