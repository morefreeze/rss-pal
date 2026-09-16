package api

import (
	"encoding/json"
	"fmt"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/sharetoken"
	"github.com/gin-gonic/gin"
	"testing"
)

func TestShareInvitationRegistration(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	publicID := "0123456789abcdef0123456789abcdef"
	_, err := db.Exec(`WITH u AS (INSERT INTO users(username,password_hash) VALUES('owner','x') RETURNING id), f AS (INSERT INTO feeds(url,title,owner_id) SELECT 'https://example.com/rss','private',id FROM u RETURNING id,owner_id), a AS (INSERT INTO articles(feed_id,url,title) SELECT id,'https://example.com/a','private' FROM f RETURNING id) INSERT INTO article_shares(public_id,article_id,created_by,snapshot,short_code,legacy_token_digest) SELECT $1,a.id,f.owner_id,'{}','o6XKeCZTDZ22',$2 FROM a,f`, publicID, sharetoken.LegacyDigest("Legacy88"))
	if err != nil {
		t.Fatal(err)
	}
	secret := "0123456789abcdef0123456789abcdef"
	signer, _ := sharetoken.NewSigner(secret)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-jwt"}, Auth: config.AuthConfig{ShareRegistrationMode: "selected_owners", ShareRegistrationOwners: "1"}, Share: config.ShareConfig{Secret: secret}}
	for _, mode := range []string{"disabled", "selected_owners", "bad-mode"} {
		deniedCfg := *cfg
		deniedCfg.Auth.ShareRegistrationMode = mode
		deniedCfg.Auth.ShareRegistrationOwners = "999"
		h := NewAuthHandler(&deniedCfg, repository.NewUserRepository(app), nil)
		h.registrationVerifier = &proofVerifier{}
		r := gin.New()
		r.POST("/register", h.Register)
		w := abuseRequest(r, "/register", `{"username":"denied","password":"test-password","share_ref":"s:o6XKeCZTDZ22","cf-turnstile-response":"fresh-proof"}`, "192.0.2.1:1234", "")
		if w.Code != 400 {
			t.Fatalf("mode %s admitted unselected owner: %d %s", mode, w.Code, w.Body.String())
		}
	}
	for i, tc := range []struct {
		name, ref, proof, code string
		want                   int
	}{
		{"short", "s:o6XKeCZTDZ22", "fresh-proof", "", 200},
		{"second reader", "s:o6XKeCZTDZ22", "fresh-proof", "", 200},
		{"signed", "t:" + signer.Sign(publicID), "fresh-proof", "", 200},
		{"legacy", "t:Legacy88", "fresh-proof", "", 200},
		{"missing proof", "s:o6XKeCZTDZ22", "", "", 403},
		{"unknown", "s:ZZZZZZZZZZZZ", "fresh-proof", "", 400},
		{"forged", "t:" + signer.Sign(publicID) + "x", "fresh-proof", "", 400},
		{"missing invite", "", "fresh-proof", "", 400},
		{"ambiguous", "s:o6XKeCZTDZ22", "fresh-proof", "ordinary", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewAuthHandler(cfg, repository.NewUserRepository(app), nil)
			h.registrationVerifier = &proofVerifier{}
			r := gin.New()
			r.POST("/register", h.Register)
			body, _ := json.Marshal(map[string]string{"username": fmt.Sprintf("reader%d", i), "password": "test-password", "code": tc.code, "share_ref": tc.ref, "cf-turnstile-response": tc.proof})
			w := abuseRequest(r, "/register", string(body), "192.0.2.1:1234", "")
			if w.Code != tc.want {
				t.Fatalf("got %d %s want %d", w.Code, w.Body.String(), tc.want)
			}
			if tc.want == 200 {
				var reply struct {
					User struct {
						ID      int  `json:"id"`
						IsAdmin bool `json:"is_admin"`
					}
					Token string
				}
				if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
					t.Fatal(err)
				}
				if reply.User.ID <= 1 || reply.User.IsAdmin || reply.Token == "" {
					t.Fatal("expected independent ordinary account")
				}
			}
		})
	}
	// A different owner remains ineligible even when promoted to administrator.
	var otherID int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash,is_admin) VALUES('other-admin','x',true) RETURNING id`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE article_shares SET created_by=$1`, otherID); err != nil {
		t.Fatal(err)
	}
	for i, mode := range []string{"selected_owners", "all", "disabled"} {
		localCfg := *cfg
		localCfg.Auth.ShareRegistrationMode = mode
		h := NewAuthHandler(&localCfg, repository.NewUserRepository(app), nil)
		h.registrationVerifier = &proofVerifier{}
		r := gin.New()
		r.POST("/register", h.Register)
		w := abuseRequest(r, "/register", fmt.Sprintf(`{"username":"other-reader%d","password":"test-password","share_ref":"s:o6XKeCZTDZ22","captcha_response":"fresh-proof"}`, i), "192.0.2.1:1234", "")
		want := 400
		if mode == "all" {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%s: %d %s", mode, w.Code, w.Body.String())
		}
	}
	if _, err := db.Exec(`UPDATE article_shares SET created_by=1`); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"revoked", "expired"} {
		t.Run(state, func(t *testing.T) {
			var err error
			if state == "revoked" {
				_, err = db.Exec(`UPDATE article_shares SET revoked_at=NOW()`)
			} else {
				_, err = db.Exec(`UPDATE article_shares SET revoked_at=NULL,created_at=NOW()-INTERVAL '2 days',expires_at=NOW()-INTERVAL '1 day'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			h := NewAuthHandler(cfg, repository.NewUserRepository(app), nil)
			h.registrationVerifier = &proofVerifier{}
			r := gin.New()
			r.POST("/register", h.Register)
			w := abuseRequest(r, "/register", `{"username":"rejected-`+state+`","password":"test-password","share_ref":"s:o6XKeCZTDZ22","cf-turnstile-response":"fresh-proof"}`, "192.0.2.1:1234", "")
			if w.Code != 400 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
