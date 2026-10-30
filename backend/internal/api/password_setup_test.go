package api

import (
	"context"
	"encoding/json"
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
	t.Run("requires old password for all later changes", func(t *testing.T) {
		for _, body := range []string{`{"new_password":"replacement"}`, `{"old_password":"wrong","new_password":"replacement"}`} {
			w := request("PUT", "/password", body, token)
			if w.Code != 401 {
				t.Fatalf("existing password changed without proof: %d %s", w.Code, w.Body.String())
			}
		}
		if w := request("PUT", "/password", `{"old_password":"new-password","new_password":"replacement"}`, token); w.Code != 200 {
			t.Fatalf("valid password change failed: %d %s", w.Code, w.Body.String())
		}
	})
}
