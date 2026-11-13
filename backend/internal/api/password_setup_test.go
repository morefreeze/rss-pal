package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestFirstPasswordSetup(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	users := repository.NewUserRepository(db)
	uid, err := repository.NewAccountAuthRepository(db).ResolveIdentity(context.Background(), "email", "password-setup@example.com", "password-setup@example.com", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	user, err := users.FindByID(uid)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(&config.Config{JWT: config.JWTConfig{Secret: "password-setup-test"}}, users, nil)
	token, err := h.generateToken(user)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/login", h.Login)
	r.Use(h.AuthMiddleware(), RLSTxMiddleware(db))
	r.GET("/me", h.GetMe)
	r.PUT("/password", h.ChangePassword)
	request := func(method, path, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	t.Run("reports missing password without exposing hash", func(t *testing.T) {
		w := request("GET", "/me", "", token)
		var state map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || state["needs_password"] != true {
			t.Fatalf("missing password state: %d %s", w.Code, w.Body.String())
		}
		if _, exists := state["password_hash"]; exists || strings.Contains(w.Body.String(), "!external") {
			t.Fatal("password hash exposed")
		}
	})
	t.Run("requires authentication and a valid new password", func(t *testing.T) {
		if w := request("PUT", "/password", `{"new_password":"new-password"}`, ""); w.Code != 401 {
			t.Fatalf("unauthorized status %d", w.Code)
		}
		for _, body := range []string{`{}`, `{"new_password":"short"}`, `{"new_password":"` + strings.Repeat("x", 73) + `"}`} {
			if w := request("PUT", "/password", body, token); w.Code != 400 {
				t.Fatalf("invalid password accepted: %d", w.Code)
			}
		}
	})
	t.Run("sets first password and allows password login", func(t *testing.T) {
		w := request("PUT", "/password", `{"new_password":"new-password"}`, token)
		if w.Code != 200 {
			t.Fatalf("first password rejected: %d %s", w.Code, w.Body.String())
		}
		var result struct {
			User struct {
				NeedsPassword bool `json:"needs_password"`
			} `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		fresh, err := users.FindByID(uid)
		if err != nil || !users.VerifyPassword(fresh, "new-password") {
			t.Fatal("first password not saved")
		}
		w = request("GET", "/me", "", token)
		var state map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &state)
		if state["needs_password"] != false {
			t.Fatalf("setup state not updated: %s", w.Body.String())
		}
		body, _ := json.Marshal(map[string]string{"username": user.Username, "password": "new-password"})
		if w = request("POST", "/login", string(body), ""); w.Code != 200 {
			t.Fatalf("password login failed: %d", w.Code)
		}
	})
	t.Run("accepts verified email after password setup", func(t *testing.T) {
		for _, email := range []string{"password-setup@example.com", " Password-Setup@Example.COM "} {
			body, _ := json.Marshal(map[string]string{"username": email, "password": "new-password"})
			w := request("POST", "/login", string(body), "")
			var result struct {
				User struct {
					ID int `json:"id"`
				} `json:"user"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || result.User.ID != uid {
				t.Fatalf("email login did not return the same account: %d %s", w.Code, w.Body.String())
			}
		}
	})
	t.Run("preserves an email-shaped legacy username", func(t *testing.T) {
		const email = "password-setup@example.com"
		legacy, err := users.CreateAdmin(email, "legacy-password")
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			password string
			id       int
		}{{"legacy-password", legacy.ID}, {"new-password", uid}} {
			body, _ := json.Marshal(map[string]string{"username": email, "password": tc.password})
			w := request("POST", "/login", string(body), "")
			var result struct {
				User struct {
					ID int `json:"id"`
				} `json:"user"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || result.User.ID != tc.id {
				t.Fatalf("login did not return account %d: %d %s", tc.id, w.Code, w.Body.String())
			}
		}
	})
	t.Run("requires an explicitly verified email for third-party accounts", func(t *testing.T) {
		ctx := context.Background()
		identities := repository.NewAccountAuthRepository(db)
		email := strings.Repeat("a", 64) + "@example.com"
		thirdPartyID, err := identities.ResolveQQIdentity(ctx, "qq-password-test", 0, true, "Reader", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = identities.ResolveIdentity(ctx, "google", "google-password-test", email, thirdPartyID, false); err != nil {
			t.Fatal(err)
		}
		if err = users.ChangePassword(thirdPartyID, "!external", "third-party-password"); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"username": email, "password": "third-party-password"})
		if w := request("POST", "/login", string(body), ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("third-party profile email accepted: %d %s", w.Code, w.Body.String())
		}
		if _, err = identities.ResolveIdentity(ctx, "email", email, email, thirdPartyID, false); err != nil {
			t.Fatal(err)
		}
		w := request("POST", "/login", string(body), "")
		var result struct {
			User struct {
				ID int `json:"id"`
			} `json:"user"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || result.User.ID != thirdPartyID {
			t.Fatalf("bound email did not return the QQ account: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("requires old password for all later changes", func(t *testing.T) {
		for _, body := range []string{`{"new_password":"replacement"}`, `{"old_password":"wrong","new_password":"replacement"}`} {
			w := request("PUT", "/password", body, token)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "旧密码不正确") {
				t.Fatalf("expected a password validation error for an authenticated user: %d %s", w.Code, w.Body.String())
			}
		}
		if w := request("PUT", "/password", `{"old_password":"new-password","new_password":"replacement"}`, token); w.Code != 200 {
			t.Fatalf("valid password change failed: %d %s", w.Code, w.Body.String())
		}
	})
}
