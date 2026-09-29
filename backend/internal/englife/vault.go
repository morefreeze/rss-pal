package englife

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

type vault struct{ aead cipher.AEAD }

func newVault(key string) (*vault, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("ENGLIFE_SESSION_KEY must be base64 encoded 32 bytes")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &vault{a}, nil
}
func (v *vault) seal(c []Cookie) ([]byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, ErrUnavailable
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, ErrUnavailable
	}
	return v.aead.Seal(nonce, nonce, raw, []byte("rss-pal:englife:v1")), nil
}
func (v *vault) open(data []byte) ([]Cookie, error) {
	n := v.aead.NonceSize()
	if len(data) < n {
		return nil, ErrExpired
	}
	raw, err := v.aead.Open(nil, data[:n], data[n:], []byte("rss-pal:englife:v1"))
	if err != nil {
		return nil, ErrExpired
	}
	var c []Cookie
	if json.Unmarshal(raw, &c) != nil {
		return nil, ErrExpired
	}
	return c, nil
}
