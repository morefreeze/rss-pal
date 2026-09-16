package api

import (
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestRegistrationDatabaseFailureIsNotInvalidInput(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewUserRepository(db)
	owner, err := repo.CreateAdmin("sequence-owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	invite, err := repo.CreateInviteCode(owner.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`SELECT setval(pg_get_serial_sequence('users','id'),$1,false)`, owner.ID); err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(&config.Config{}, repo, nil)
	h.registrationVerifier = &proofVerifier{}
	r := gin.New()
	r.POST("/register", h.Register)
	body := `{"username":"sequence-reader","password":"test-password","code":"` + invite.Code + `","captcha_response":"fresh-proof"}`
	w := abuseRequest(r, "/register", body, "192.0.2.1:1234", "")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "注册服务暂时不可用") {
		t.Fatalf("database failure misclassified: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "users_pkey") {
		t.Fatal("database details exposed")
	}
	var used *int
	if err := db.QueryRow(`SELECT used_by FROM invite_codes WHERE id=$1`, invite.ID).Scan(&used); err != nil || used != nil {
		t.Fatalf("invite consumed: %v", err)
	}
}
