package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/nickname"
	"golang.org/x/oauth2"
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

type qqAccountProfile struct {
	Subject   string
	Nickname  string
	AvatarURL string
}

func qqAvatarURL(candidates ...string) string {
	for _, raw := range candidates {
		if len(raw) > 2048 {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host != "qlogo.cn" && !strings.HasSuffix(host, ".qlogo.cn") {
			continue
		}
		u.Scheme = "https"
		u.Host = host
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	return ""
}

func qqAccount(ctx context.Context, client *http.Client, o *oauth2.Config, code string) (qqAccountProfile, error) {
	var token struct {
		AccessToken string `json:"access_token"`
		Error       *int   `json:"error"`
	}
	err := qqGetJSON(ctx, client, "https://graph.qq.com/oauth2.0/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {o.ClientID}, "client_secret": {o.ClientSecret},
		"code": {code}, "redirect_uri": {o.RedirectURL}, "fmt": {"json"},
	}, &token)
	if err != nil || token.Error != nil || token.AccessToken == "" || len(token.AccessToken) > 8192 {
		return qqAccountProfile{}, errors.New("QQ token exchange failed")
	}
	var identity struct {
		ClientID string `json:"client_id"`
		OpenID   string `json:"openid"`
		Error    *int   `json:"error"`
	}
	if err := qqGetJSON(ctx, client, "https://graph.qq.com/oauth2.0/me", url.Values{"access_token": {token.AccessToken}, "fmt": {"json"}}, &identity); err != nil {
		return qqAccountProfile{}, err
	}
	if identity.Error != nil || identity.ClientID != o.ClientID || identity.OpenID == "" || len(identity.OpenID) > 256 || strings.IndexFunc(identity.OpenID, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		return qqAccountProfile{}, errors.New("invalid QQ identity")
	}
	var profile struct {
		Ret         *int   `json:"ret"`
		IsLost      int    `json:"is_lost"`
		Nickname    string `json:"nickname"`
		Avatar100   string `json:"figureurl_qq_2"`
		Avatar40    string `json:"figureurl_qq_1"`
		QzoneAvatar string `json:"figureurl_2"`
	}
	if err := qqGetJSON(ctx, client, "https://graph.qq.com/user/get_user_info", url.Values{
		"access_token": {token.AccessToken}, "oauth_consumer_key": {o.ClientID}, "openid": {identity.OpenID}, "fmt": {"json"},
	}, &profile); err != nil || profile.Ret == nil || *profile.Ret != 0 || profile.IsLost != 0 {
		return qqAccountProfile{}, errors.New("QQ user info unavailable")
	}
	name := strings.TrimSpace(profile.Nickname)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 || strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '\u200d' && r != '\u200c')
	}) {
		return qqAccountProfile{}, errors.New("invalid QQ user info")
	}
	// OpenID remains the sole identity key; profile fields are display metadata.
	return qqAccountProfile{Subject: o.ClientID + ":" + identity.OpenID, Nickname: nickname.QQ(name), AvatarURL: qqAvatarURL(profile.Avatar100, profile.Avatar40, profile.QzoneAvatar)}, nil
}
