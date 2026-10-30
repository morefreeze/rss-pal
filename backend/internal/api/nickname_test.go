package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestNicknameSetup(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	users := repository.NewUserRepository(db)
	uid, err := repository.NewAccountAuthRepository(db).ResolveIdentity(context.Background(), "email", "nickname@example.com", "nickname@example.com", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(&config.Config{JWT: config.JWTConfig{Secret: "nickname-test"}}, users, nil)
	r := gin.New()
	r.Use(h.AuthMiddleware(), RLSTxMiddleware(db))
	r.GET("/me", h.GetMe)
	r.PUT("/nickname", h.SetNickname)
	u, err := users.FindByID(uid)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := h.generateToken(u)
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
	w := request("GET", "/me", "", token)
	var before map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &before)
	if before["needs_nickname"] != true || before["nickname"] != "" {
		t.Fatalf("missing onboarding: %s", w.Body.String())
	}
	if w := request("PUT", "/nickname", `{"nickname":"读者","confirmed":true}`, ""); w.Code != 401 {
		t.Fatalf("unauthorized %d", w.Code)
	}
	for _, body := range []string{`{}`, `{"nickname":"读者"}`, `{"nickname":"   ","confirmed":true}`, `{"nickname":"bad\nname","confirmed":true}`, `{"nickname":"` + strings.Repeat("字", 33) + `","confirmed":true}`, `{"nickname":"admin","confirmed":true}`, `{"nickname":"ＡＤＭＩＮ_01","confirmed":true}`, `{"nickname":"官方客服","confirmed":true}`, `{"nickname":"f.u.c.k","confirmed":true}`} {
		if w := request("PUT", "/nickname", body, token); w.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	// Two requests from the same account must never overwrite one another.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, name := range []string{"读者甲", "读者乙"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			codes <- request("PUT", "/nickname", `{"nickname":"  `+name+`  ","confirmed":true}`, token).Code
		}(name)
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent results %v", counts)
	}
	w = request("GET", "/me", "", token)
	var after model.User
	if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.Username != u.Username || after.ID != uid || after.Nickname == "" || after.NeedsNickname {
		t.Fatalf("invalid persisted profile %s", w.Body.String())
	}
	other, err := users.CreateAdmin("other-user", "password")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _ := h.generateToken(other)
	if w := request("PUT", "/nickname", `{"nickname":"另一个读者","confirmed":true,"user_id":`+strconv.Itoa(uid)+`}`, otherToken); w.Code != 200 {
		t.Fatalf("other account setup %d %s", w.Code, w.Body.String())
	}
	fresh, _ := users.FindByID(uid)
	if fresh.Nickname != after.Nickname {
		t.Fatal("another user changed nickname")
	}
}

func TestNicknameCommitFailure(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	users := repository.NewUserRepository(db)
	user, err := users.CreateAdmin("commit-reader", "password")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE FUNCTION reject_nickname_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test commit rejected'; END $$;
 CREATE CONSTRAINT TRIGGER reject_nickname_commit AFTER UPDATE ON users DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_nickname_commit();`)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(&config.Config{JWT: config.JWTConfig{Secret: "nickname-commit"}}, users, nil)
	token, _ := h.generateToken(user)
	r := gin.New()
	r.Use(h.AuthMiddleware(), RLSTxMiddleware(db))
	r.PUT("/nickname", h.SetNickname)
	req := httptest.NewRequest("PUT", "/nickname", strings.NewReader(`{"nickname":"不可虚报成功","confirmed":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code < 500 {
		t.Fatalf("commit failure returned success: %d %s", w.Code, w.Body.String())
	}
	fresh, err := users.FindByID(user.ID)
	if err != nil || fresh.Nickname != "" {
		t.Fatalf("failed transaction persisted: %v %v", fresh, err)
	}
}
