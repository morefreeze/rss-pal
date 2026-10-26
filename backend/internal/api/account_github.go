package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// GitHub OAuth is not OIDC. Resolve the current authenticated user's stable ID
// via the fixed API endpoint; never treat public email or mutable login as identity.
func githubSubject(ctx context.Context, client *http.Client, token string) (string, error) {
	if token == "" {
		return "", errors.New("missing GitHub access token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "RSS-Pal")
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("GitHub identity request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errors.New("GitHub identity unavailable")
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&user); err != nil || user.ID <= 0 {
		return "", errors.New("invalid GitHub identity")
	}
	return strconv.FormatInt(user.ID, 10), nil
}
