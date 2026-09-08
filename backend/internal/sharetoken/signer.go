package sharetoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	minimumSecretBytes = 32
	publicIDBytes      = 16
	legacyTokenLength  = 8
)

type Signer struct {
	secret []byte
}

func NewSigner(secret string) (*Signer, error) {
	if len(secret) < minimumSecretBytes {
		return nil, fmt.Errorf("share secret must be at least %d bytes", minimumSecretBytes)
	}
	return &Signer{secret: []byte(secret)}, nil
}

func NewPublicID() (string, error) {
	random := make([]byte, publicIDBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate public ID: %w", err)
	}
	return hex.EncodeToString(random), nil
}

func (s *Signer) Sign(publicID string) string {
	signature := s.signature(publicID)
	return "v1_" + publicID + "_" + base64.RawURLEncoding.EncodeToString(signature)
}

func (s *Signer) Parse(token string) (value string, legacy bool, err error) {
	if isLegacyToken(token) {
		return token, true, nil
	}

	parts := strings.Split(token, "_")
	if len(parts) != 3 || parts[0] != "v1" {
		return "", false, errors.New("invalid share token format")
	}
	publicID := parts[1]
	if len(publicID) != publicIDBytes*2 {
		return "", false, errors.New("invalid share token public ID")
	}
	if _, err := hex.DecodeString(publicID); err != nil {
		return "", false, errors.New("invalid share token public ID")
	}

	provided, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		return "", false, errors.New("invalid share token signature")
	}
	if base64.RawURLEncoding.EncodeToString(provided) != parts[2] {
		return "", false, errors.New("invalid share token signature")
	}
	if !hmac.Equal(provided, s.signature(publicID)) {
		return "", false, errors.New("invalid share token signature")
	}
	return publicID, false, nil
}

func LegacyDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (s *Signer) signature(publicID string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte("v1:" + publicID))
	return mac.Sum(nil)
}

func isLegacyToken(token string) bool {
	if len(token) != legacyTokenLength {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
