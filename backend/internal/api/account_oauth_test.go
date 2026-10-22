package api

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthIDTokenRejectsInvalidClaimsAndSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	wrong, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := oidc.NewVerifier("https://accounts.google.com", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "our-client", SupportedSigningAlgs: []string{oidc.RS256}})
	cases := []struct {
		name     string
		alter    func(jwt.MapClaims)
		wrongKey bool
		ok       bool
	}{
		{"valid", func(jwt.MapClaims) {}, false, true},
		{"issuer", func(c jwt.MapClaims) { c["iss"] = "https://attacker.test" }, false, false},
		{"audience", func(c jwt.MapClaims) { c["aud"] = "other-client" }, false, false},
		{"expiry", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, false, false},
		{"nonce", func(c jwt.MapClaims) { c["nonce"] = "other-browser" }, false, false},
		{"subject", func(c jwt.MapClaims) { c["sub"] = "" }, false, false},
		{"signature", func(jwt.MapClaims) {}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := jwt.MapClaims{"iss": "https://accounts.google.com", "aud": "our-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "our-nonce", "sub": "stable-sub", "email": "relay@privaterelay.appleid.com", "email_verified": true}
			tc.alter(c)
			k := key
			if tc.wrongKey {
				k = wrong
			}
			raw, e := jwt.NewWithClaims(jwt.SigningMethodRS256, c).SignedString(k)
			if e != nil {
				t.Fatal(e)
			}
			sub, email, e := verifyOAuthClaims(context.Background(), v, raw, "our-nonce")
			if tc.ok {
				if e != nil || sub != "stable-sub" || email != "relay@privaterelay.appleid.com" {
					t.Fatalf("valid token rejected %q %q %v", sub, email, e)
				}
			} else if e == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
}

type authFixtureTransport func(*http.Request) (*http.Response, error)

func (f authFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOAuthStartCallbackProviderWiring(t *testing.T) {
	for _, provider := range []string{"google", "apple"} {
		t.Run(provider, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-jwt"}}
			cfg.Auth.PublicRegistration = true
			cfg.Auth.PublicOrigin = "https://app.example.com"
			cfg.Auth.GoogleClientID = "google-client"
			cfg.Auth.GoogleClientSecret = "google-secret"
			cfg.Auth.AppleClientID = "apple-service"
			cfg.Auth.AppleTeamID = "apple-team"
			cfg.Auth.AppleKeyID = "apple-key"
			appleKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			der, _ := x509.MarshalECPrivateKey(appleKey)
			cfg.Auth.ApplePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
			h := NewAccountAuthHandler(NewAuthHandler(cfg, repository.NewUserRepository(db), nil), repository.NewAccountAuthRepository(db))
			router := gin.New()
			router.POST("/api/auth/oauth/:provider/start", h.OAuthStart)
			router.GET("/api/auth/oauth/:provider/callback", h.OAuthCallback)
			router.POST("/api/auth/oauth/:provider/callback", h.OAuthCallback)
			router.POST("/exchange", h.OAuthExchange)
			start := abuseRequest(router, "/api/auth/oauth/"+provider+"/start", `{"return_to":"/capture?url=https%3A%2F%2Fexample.com","remember":false}`, "192.0.2.1:12", "")
			if start.Code != 200 {
				t.Fatal(start.Code, start.Body.String())
			}
			var resp struct {
				URL string `json:"url"`
			}
			json.Unmarshal(start.Body.Bytes(), &resp)
			authorize, _ := url.Parse(resp.URL)
			q := authorize.Query()
			state, nonce := q.Get("state"), q.Get("nonce")
			if state == "" || nonce == "" {
				t.Fatal("missing state or nonce")
			}
			cookies := start.Result().Cookies()
			if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteNoneMode {
				t.Fatal("Apple-compatible secure browser cookie missing")
			}
			var flowJSON []byte
			if err = db.QueryRow(`SELECT payload FROM account_auth_flows WHERE kind='state'`).Scan(&flowJSON); err != nil {
				t.Fatal(err)
			}
			var flow accountOAuthFlow
			json.Unmarshal(flowJSON, &flow)
			if provider == "google" {
				if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(flow.Verifier) {
					t.Fatal("PKCE challenge missing")
				}
			} else if q.Get("response_mode") != "form_post" {
				t.Fatal("Apple must use form_post")
			}
			signingKey, _ := rsa.GenerateKey(rand.Reader, 2048)
			issuer := "https://accounts.google.com"
			aud := "google-client"
			if provider == "apple" {
				issuer = "https://appleid.apple.com"
				aud = "apple-service"
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "aud": aud, "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "sub": "provider-subject", "nonce": nonce, "email": "relay@privaterelay.appleid.com", "email_verified": "true"})
			token.Header["kid"] = "fixture"
			idtoken, _ := token.SignedString(signingKey)
			tokenRequests := 0
			h.httpClient = &http.Client{Transport: authFixtureTransport(func(req *http.Request) (*http.Response, error) {
				var body string
				switch req.URL.Host + req.URL.Path {
				case "oauth2.googleapis.com/token", "appleid.apple.com/auth/token":
					tokenRequests++
					req.ParseForm()
					if req.Form.Get("code") != "provider-code" || req.Form.Get("redirect_uri") != cfg.Auth.PublicOrigin+"/api/auth/oauth/"+provider+"/callback" {
						t.Error("wrong exchange code/callback")
					}
					if provider == "google" {
						if req.Form.Get("code_verifier") != flow.Verifier {
							t.Error("PKCE verifier missing")
						}
					} else {
						claims := &jwt.RegisteredClaims{}
						parsed, e := jwt.ParseWithClaims(req.Form.Get("client_secret"), claims, func(tok *jwt.Token) (any, error) {
							if tok.Method != jwt.SigningMethodES256 || tok.Header["kid"] != "apple-key" {
								t.Error("wrong Apple signing algorithm or key ID")
							}
							return &appleKey.PublicKey, nil
						}, jwt.WithValidMethods([]string{"ES256"}))
						if e != nil || !parsed.Valid || claims.Issuer != "apple-team" || claims.Subject != "apple-service" || !claims.VerifyAudience("https://appleid.apple.com", true) || time.Until(claims.ExpiresAt.Time) > 5*time.Minute {
							t.Errorf("invalid Apple client secret: %v", e)
						}
					}
					b, _ := json.Marshal(map[string]any{"access_token": "fixture-access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": idtoken})
					body = string(b)
				case "www.googleapis.com/oauth2/v3/certs", "appleid.apple.com/auth/keys":
					b, _ := json.Marshal(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "fixture", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(signingKey.N.Bytes()), "e": "AQAB"}}})
					body = string(b)
				default:
					t.Fatalf("unexpected external endpoint %s", req.URL.String())
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			callback := func(withCookie bool) *httptest.ResponseRecorder {
				vals := url.Values{"state": []string{state}, "code": []string{"provider-code"}}
				var req *http.Request
				if provider == "apple" {
					req = httptest.NewRequest("POST", "/api/auth/oauth/apple/callback", strings.NewReader(vals.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				} else {
					req = httptest.NewRequest("GET", "/api/auth/oauth/google/callback?"+vals.Encode(), nil)
				}
				if withCookie {
					req.AddCookie(cookies[0])
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			if w := callback(false); !strings.Contains(w.Header().Get("Location"), "error=oauth_failed") || tokenRequests != 0 {
				t.Fatal("cookie-free callback accepted")
			}
			w := callback(true)
			if w.Code != 303 {
				t.Fatal(w.Code, w.Body.String())
			}
			complete, _ := url.Parse(w.Header().Get("Location"))
			ticket := complete.Query().Get("ticket")
			if len(ticket) != 43 || strings.Contains(complete.String(), "fixture-access-token") || complete.Query().Get("token") != "" {
				t.Fatal("unsafe completion redirect", complete.String())
			}
			if w := callback(true); !strings.Contains(w.Header().Get("Location"), "error=oauth_failed") || tokenRequests != 1 {
				t.Fatal("state replay accepted")
			}
			req := httptest.NewRequest("POST", "/exchange", strings.NewReader(`{"ticket":"`+ticket+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookies[0])
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
				t.Fatal("completion exchange failed", w.Code, w.Body.String())
			}
		})
	}
}
