package airouting

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrConflict = errors.New("配置已被其他管理员修改，请重新加载后再保存")

type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }
func invalid(s string) error          { return &InvalidError{s} }

type Selection struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
type CredentialInput struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
}
type Update struct {
	Revision    int64                      `json:"revision"`
	Threshold   int                        `json:"threshold"`
	Small       Selection                  `json:"small"`
	Large       Selection                  `json:"large"`
	Credentials map[string]CredentialInput `json:"credentials"`
}
type ProviderView struct {
	Provider
	Endpoint string `json:"endpoint"`
	HasKey   bool   `json:"has_key"`
}
type View struct {
	Revision  int64          `json:"revision"`
	Threshold int            `json:"threshold"`
	Small     Selection      `json:"small"`
	Large     Selection      `json:"large"`
	Providers []ProviderView `json:"providers"`
}
type credential struct {
	Endpoint   string `json:"endpoint"`
	Ciphertext string `json:"ciphertext,omitempty"`
	Inherited  bool   `json:"inherited,omitempty"`
}
type state struct {
	Threshold   int                   `json:"threshold"`
	Small       Selection             `json:"small"`
	Large       Selection             `json:"large"`
	Credentials map[string]credential `json:"credentials"`
}
type Target struct {
	Provider, Model, APIKey, BaseURL, Protocol string
	Large                                      bool
}
type Store struct {
	db       *sql.DB
	key      [32]byte
	fallback Target
	defaults state
}

func NewStore(db *sql.DB, secret, apiKey, baseURL, model string) *Store {
	if model == "" {
		model = "glm-5.3"
	}
	s := &Store{db: db, key: sha256.Sum256([]byte("rss-pal/platform-ai/v1/" + secret)), fallback: Target{Model: model, APIKey: apiKey, BaseURL: strings.TrimRight(baseURL, "/"), Protocol: "openai"}}
	provider, endpoint := "zai", "coding"
	for _, p := range Providers {
		for _, e := range p.Endpoints {
			if e.BaseURL == s.fallback.BaseURL {
				provider, endpoint = p.ID, e.ID
				s.fallback.Provider = p.ID
				s.fallback.Protocol = p.Protocol
			}
		}
	}
	small, large := model, model
	if provider == "zai" && (model == "glm-5.3" || model == "glm-5.3-flash") {
		small = "glm-5.3-flash"
		large = "glm-5.3"
	}
	s.defaults = state{10000, Selection{provider, small}, Selection{provider, large}, map[string]credential{provider: {Endpoint: endpoint, Inherited: true}}}
	return s
}
func (s *Store) aead() (cipher.AEAD, error) {
	block, e := aes.NewCipher(s.key[:])
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func (s *Store) seal(provider, key string) (string, error) {
	a, e := s.aead()
	if e != nil {
		return "", e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	data := a.Seal(nonce, nonce, []byte(key), []byte(provider))
	return base64.StdEncoding.EncodeToString(data), nil
}
func (s *Store) open(provider, encoded string) (string, error) {
	a, e := s.aead()
	if e != nil {
		return "", e
	}
	b, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil || len(b) < a.NonceSize() {
		return "", errors.New("invalid encrypted provider key")
	}
	b, e = a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(provider))
	if e != nil {
		return "", errors.New("provider key decryption failed")
	}
	return string(b), nil
}
func (s *Store) decode(raw []byte) (state, error) {
	if len(raw) == 0 || string(raw) == "null" {
		b, _ := json.Marshal(s.defaults)
		raw = b
	}
	var c state
	e := json.Unmarshal(raw, &c)
	if c.Credentials == nil {
		c.Credentials = map[string]credential{}
	}
	return c, e
}
func (s *Store) load(ctx context.Context) (state, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var raw []byte
	var rev int64
	e := s.db.QueryRowContext(ctx, `SELECT revision,config FROM platform_ai_config WHERE id=true`).Scan(&rev, &raw)
	if e != nil {
		return state{}, 0, e
	}
	c, e := s.decode(raw)
	return c, rev, e
}
func (s *Store) keyFor(provider string, c credential) (string, error) {
	if c.Ciphertext != "" {
		return s.open(provider, c.Ciphertext)
	}
	if c.Inherited {
		_, e, err := Lookup(provider, c.Endpoint)
		if err == nil && e.BaseURL == s.fallback.BaseURL {
			return s.fallback.APIKey, nil
		}
	}
	return "", nil
}
func (s *Store) view(c state, rev int64) (View, error) {
	v := View{Revision: rev, Threshold: c.Threshold, Small: c.Small, Large: c.Large, Providers: []ProviderView{}}
	for _, p := range Providers {
		cred := c.Credentials[p.ID]
		if cred.Endpoint == "" {
			cred.Endpoint = p.Endpoints[0].ID
		}
		key, e := s.keyFor(p.ID, cred)
		if e != nil {
			return View{}, e
		}
		v.Providers = append(v.Providers, ProviderView{p, cred.Endpoint, key != ""})
	}
	return v, nil
}
func (s *Store) View(ctx context.Context) (View, error) {
	c, rev, e := s.load(ctx)
	if e != nil {
		return View{}, e
	}
	return s.view(c, rev)
}
func (s *Store) Key(ctx context.Context, provider, endpoint, draft string) (string, error) {
	if _, _, e := Lookup(provider, endpoint); e != nil {
		return "", invalid(e.Error())
	}
	draft = strings.TrimSpace(draft)
	if len(draft) > 4096 || strings.ContainsAny(draft, "\r\n") {
		return "", invalid("API Key 格式无效")
	}
	if draft != "" {
		return draft, nil
	}
	c, _, e := s.load(ctx)
	if e != nil {
		return "", e
	}
	cred := c.Credentials[provider]
	if cred.Endpoint != endpoint {
		return "", invalid("该接入方式尚未配置 API Key")
	}
	key, e := s.keyFor(provider, cred)
	if e != nil {
		return "", e
	}
	if key == "" {
		return "", invalid("请先填写该公司的 API Key")
	}
	return key, nil
}
func (s *Store) Save(ctx context.Context, u Update) (View, error) {
	if u.Threshold < 1 || u.Threshold > 100000 {
		return View{}, invalid("文章分界字数须为 1～100000")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback()
	var rev int64
	var raw []byte
	if e = tx.QueryRowContext(ctx, `SELECT revision,config FROM platform_ai_config WHERE id=true FOR UPDATE`).Scan(&rev, &raw); e != nil {
		return View{}, e
	}
	if rev != u.Revision {
		return View{}, ErrConflict
	}
	c, e := s.decode(raw)
	if e != nil {
		return View{}, e
	}
	for provider, input := range u.Credentials {
		if _, _, e = Lookup(provider, input.Endpoint); e != nil {
			return View{}, invalid(e.Error())
		}
		old := c.Credentials[provider]
		key := strings.TrimSpace(input.APIKey)
		if len(key) > 4096 || strings.ContainsAny(key, "\r\n") {
			return View{}, invalid("API Key 格式无效")
		}
		if key != "" {
			sealed, e := s.seal(provider, key)
			if e != nil {
				return View{}, e
			}
			c.Credentials[provider] = credential{Endpoint: input.Endpoint, Ciphertext: sealed}
		} else if old.Endpoint != input.Endpoint {
			if old.Ciphertext != "" || old.Inherited {
				return View{}, invalid("更换接入方式时，请填写对应的 API Key")
			}
			c.Credentials[provider] = credential{Endpoint: input.Endpoint}
		}
	}
	for _, sel := range []Selection{u.Small, u.Large} {
		cred := c.Credentials[sel.Provider]
		if _, _, e = Lookup(sel.Provider, cred.Endpoint); e != nil {
			return View{}, invalid("请先配置所选公司的接入方式和 API Key")
		}
		if !ValidModel(sel.Provider, sel.Model) {
			return View{}, invalid("请选择该公司的文章摘要模型")
		}
		key, e := s.keyFor(sel.Provider, cred)
		if e != nil {
			return View{}, e
		}
		if key == "" {
			return View{}, invalid("所选公司的 API Key 尚未配置")
		}
	}
	c.Threshold = u.Threshold
	c.Small = u.Small
	c.Large = u.Large
	raw, e = json.Marshal(c)
	if e != nil {
		return View{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE platform_ai_config SET revision=revision+1,config=$1,updated_at=clock_timestamp() WHERE id=true`, string(raw)); e != nil {
		return View{}, e
	}
	v, e := s.view(c, rev+1)
	if e != nil {
		return View{}, e
	}
	if e = tx.Commit(); e != nil {
		return View{}, e
	}
	return v, nil
}
func (s *Store) Resolve(ctx context.Context, length int) (Target, error) {
	c, rev, e := s.load(ctx)
	if e != nil {
		return Target{}, fmt.Errorf("读取后台 AI 配置失败: %w", e)
	}
	// An unrecognised legacy server endpoint remains unchanged until an admin
	// explicitly saves the new configuration.
	if rev == 0 && s.fallback.Provider == "" {
		return s.fallback, nil
	}
	large := length > c.Threshold
	sel := c.Small
	if large {
		sel = c.Large
	}
	cred := c.Credentials[sel.Provider]
	p, endpoint, e := Lookup(sel.Provider, cred.Endpoint)
	if e != nil {
		return Target{}, e
	}
	key, e := s.keyFor(sel.Provider, cred)
	if e != nil {
		return Target{}, e
	}
	if key == "" {
		return Target{}, errors.New("所选 AI 公司尚未配置 API Key")
	}
	return Target{Provider: p.ID, Model: sel.Model, APIKey: key, BaseURL: endpoint.BaseURL, Protocol: p.Protocol, Large: large}, nil
}
