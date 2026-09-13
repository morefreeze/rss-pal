package sharetoken

import (
	"crypto/rand"
	"fmt"
	"io"
)

const (
	ShortCodeLength   = 12
	shortCodeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	shortCodeLimit    = 256 - (256 % len(shortCodeAlphabet))
)

func NewShortCode() (string, error) {
	return newShortCode(rand.Reader)
}

func newShortCode(random io.Reader) (string, error) {
	code := make([]byte, 0, ShortCodeLength)
	value := make([]byte, 1)
	for len(code) < ShortCodeLength {
		if _, err := io.ReadFull(random, value); err != nil {
			return "", fmt.Errorf("generate short code: %w", err)
		}
		if int(value[0]) >= shortCodeLimit {
			continue
		}
		code = append(code, shortCodeAlphabet[int(value[0])%len(shortCodeAlphabet)])
	}
	return string(code), nil
}

func IsValidShortCode(code string) bool {
	if len(code) != ShortCodeLength {
		return false
	}
	for i := 0; i < len(code); i++ {
		char := code[i]
		if !((char >= '0' && char <= '9') || (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z')) {
			return false
		}
	}
	return true
}
