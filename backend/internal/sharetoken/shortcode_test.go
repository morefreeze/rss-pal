package sharetoken

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNewShortCodeRejectsOutOfRangeBytes(t *testing.T) {
	random := append([]byte{248, 249, 250, 251, 252, 253, 254, 255}, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}...)

	got, err := newShortCode(bytes.NewReader(random))
	if err != nil {
		t.Fatalf("newShortCode() error = %v", err)
	}
	if got != "0123456789AB" {
		t.Fatalf("newShortCode() = %q, want %q", got, "0123456789AB")
	}
}

func TestNewShortCodePropagatesRandomSourceError(t *testing.T) {
	wantErr := errors.New("random source failed")

	_, err := newShortCode(errorReader{err: wantErr})
	if err == nil {
		t.Fatal("newShortCode() error = nil, want random source error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("newShortCode() error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "generate short code") {
		t.Fatalf("newShortCode() error = %q, want generate short code context", err)
	}
}

func TestNewShortCodeReturnsValidCode(t *testing.T) {
	code, err := NewShortCode()
	if err != nil {
		t.Fatalf("NewShortCode() error = %v", err)
	}
	if len(code) != ShortCodeLength {
		t.Fatalf("len(NewShortCode()) = %d, want %d", len(code), ShortCodeLength)
	}
	if !IsValidShortCode(code) {
		t.Fatalf("NewShortCode() = %q, want a valid short code", code)
	}
}

func TestIsValidShortCode(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{name: "valid", code: "0Aa9Zz1Bb8Yy", want: true},
		{name: "too short", code: "0Aa9Zz1Bb8Y", want: false},
		{name: "too long", code: "0Aa9Zz1Bb8YyX", want: false},
		{name: "underscore", code: "0Aa9Zz1Bb8Y_", want: false},
		{name: "hyphen", code: "0Aa9Zz1Bb8Y-", want: false},
		{name: "non ASCII", code: "0Aa9Zz1Bb8Yé", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidShortCode(tt.code); got != tt.want {
				t.Fatalf("IsValidShortCode(%q) = %t, want %t", tt.code, got, tt.want)
			}
		})
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}
