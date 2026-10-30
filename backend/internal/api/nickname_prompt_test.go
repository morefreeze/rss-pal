package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestNicknamePromptPersistsAcrossLogins(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	users := repository.NewUserRepository(db)
	accounts := repository.NewAccountAuthRepository(db)
	owner, err := accounts.ResolveIdentity(context.Background(), "email", "prompt@example.com", "prompt@example.com", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := accounts.ResolveIdentity(context.Background(), "email", "other-prompt@example.com", "other-prompt@example.com", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(&config.Config{JWT: config.JWTConfig{Secret: "nickname-prompt"}}, users, nil)
	u, err := users.FindByID(owner)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := h.generateToken(u)
	r := gin.New()
	r.Use(h.AuthMiddleware(), RLSTxMiddleware(db))
	r.PUT("/nickname/prompted", h.MarkNicknamePrompted)
	r.PUT("/nickname", h.SetNickname)
	r.GET("/me", h.GetMe)
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
	if !u.NeedsNickname {
		t.Fatal("new account should need the nickname prompt")
	}
	if w := request("PUT", "/nickname/prompted", "{}", ""); w.Code != 401 {
		t.Fatalf("unauthorized status %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := request("PUT", "/nickname/prompted", `{"user_id":`+strconv.Itoa(other)+`}`, token); w.Code != 200 {
			t.Fatalf("acknowledgment failed: %d %s", w.Code, w.Body.String())
		}
	}
	// A fresh token and a new read model a later login from another device.
	token, _ = h.generateToken(u)
	w := request("GET", "/me", "", token)
	var fresh model.User
	if err := json.Unmarshal(w.Body.Bytes(), &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.NeedsNickname || fresh.Nickname != "" || fresh.Username != u.Username {
		t.Fatalf("prompt was not persisted independently of name: %s", w.Body.String())
	}
	untouched, err := users.FindByID(other)
	if err != nil || !untouched.NeedsNickname {
		t.Fatal("acknowledgment affected another account")
	}
	// Deployment replays the legacy migration, which must not re-enable a seen prompt.
	for _, migration := range []string{"066_user_nickname.sql", "071_account_onboarding.sql", "071_account_onboarding.sql"} {
		if err := testdb.ExecuteMigrationFile(db, migration); err != nil {
			t.Fatal(err)
		}
	}
	freshUser, err := users.FindByID(owner)
	if err != nil || freshUser.NeedsNickname {
		t.Fatal("migration replay re-enabled a seen prompt")
	}
	// Acknowledging or skipping the prompt must still allow setting a nickname in settings.
	if w := request("PUT", "/nickname", `{"nickname":"稍后设置的昵称","confirmed":true}`, token); w.Code != 200 {
		t.Fatalf("settings nickname rejected: %d %s", w.Code, w.Body.String())
	}
}
