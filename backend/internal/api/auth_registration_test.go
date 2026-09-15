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

type proofVerifier struct{ used bool }

func (v *proofVerifier) Verify(_ context.Context, token, ip string) error {
	if token != "fresh-proof" || v.used {
		return ErrVerificationRequired
	}
	v.used = true
	return nil
}

func TestRegistrationRequiresProofBeforeInviteConsumption(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewUserRepository(db)
	admin, err := repo.CreateAdmin("register-admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	invite, err := repo.CreateInviteCode(admin.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(&config.Config{JWT: config.JWTConfig{Secret: "test-key"}}, repo, nil)
	h.registrationVerifier = &proofVerifier{}
	r := gin.New()
	r.POST("/register", h.Register)
	body := `{"username":"new-user","password":"test-password","code":"` + invite.Code + `"}`
	w := abuseRequest(r, "/register", body, "192.0.2.1:1234", "")
	if w.Code != 403 {
		t.Fatalf("missing proof returned %d %s", w.Code, w.Body.String())
	}
	w = abuseRequest(r, "/register", strings.TrimSuffix(body, "}")+`,"cf-turnstile-response":"fresh-proof"}`, "192.0.2.1:1234", "")
	if w.Code != 200 {
		t.Fatalf("valid proof returned %d %s", w.Code, w.Body.String())
	}
	second, err := repo.CreateInviteCode(admin.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	w = abuseRequest(r, "/register", `{"username":"replayed","password":"test-password","code":"`+second.Code+`","cf-turnstile-response":"fresh-proof"}`, "192.0.2.1:1234", "")
	if w.Code != 403 {
		t.Fatalf("replayed proof returned %d", w.Code)
	}
	var used *int
	if err := db.QueryRow(`SELECT used_by FROM invite_codes WHERE id=$1`, second.ID).Scan(&used); err != nil || used != nil {
		t.Fatalf("replay consumed invite: %v %v", used, err)
	}
}
func TestPublicBootstrapNeverReturnsAdministratorCredentials(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	h := NewAuthHandler(&config.Config{Auth: config.AuthConfig{Password: "test-password"}, JWT: config.JWTConfig{Secret: "test-key"}}, repository.NewUserRepository(db), nil)
	r := gin.New()
	r.POST("/init", h.InitAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/init", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["token"] != nil || result["user"] != nil {
		t.Fatal("unauthenticated bootstrap returned administrator credentials")
	}
}
