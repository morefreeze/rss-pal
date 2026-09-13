package config

import "testing"

func TestLoadShareSecret(t *testing.T) {
	t.Setenv("SHARE_SECRET", "share-secret-for-test")
	t.Setenv("SHORT_SHARE_ORIGIN", "https://short.example.test")

	cfg := Load()
	if got := cfg.Share.Secret; got != "share-secret-for-test" {
		t.Fatalf("Load().Share.Secret = %q, want %q", got, "share-secret-for-test")
	}
	if got := cfg.Share.ShortOrigin; got != "https://short.example.test" {
		t.Fatalf("Load().Share.ShortOrigin = %q, want %q", got, "https://short.example.test")
	}
}

func TestValidateShortShareOriginAcceptsHTTPSOrigins(t *testing.T) {
	for _, raw := range []string{
		"https://r.morefreeze.top",
		"https://short.example.test:8443",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := ValidateShortShareOrigin(raw)
			if err != nil {
				t.Fatalf("ValidateShortShareOrigin(%q) error = %v", raw, err)
			}
			if got != raw {
				t.Fatalf("ValidateShortShareOrigin(%q) = %q, want unchanged origin", raw, got)
			}
		})
	}
}

func TestValidateShortShareOriginRejectsNonOrigins(t *testing.T) {
	const wantErr = "SHORT_SHARE_ORIGIN must be an HTTPS origin without path, query, fragment, or credentials"
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "http", raw: "http://short.example.test"},
		{name: "trailing slash", raw: "https://short.example.test/"},
		{name: "path", raw: "https://short.example.test/share"},
		{name: "credentials", raw: "https://user:pass@short.example.test"},
		{name: "query", raw: "https://short.example.test?source=rss"},
		{name: "force query", raw: "https://short.example.test?"},
		{name: "fragment", raw: "https://short.example.test#share"},
		{name: "port out of range", raw: "https://short.example.test:65536"},
		{name: "port overflows integer", raw: "https://short.example.test:9999999999999999999999999999999999999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateShortShareOrigin(tt.raw)
			if err == nil {
				t.Fatalf("ValidateShortShareOrigin(%q) = %q, nil; want error", tt.raw, got)
			}
			if got != "" {
				t.Fatalf("ValidateShortShareOrigin(%q) = %q, want empty result", tt.raw, got)
			}
			if err.Error() != wantErr {
				t.Fatalf("ValidateShortShareOrigin(%q) error = %q, want %q", tt.raw, err, wantErr)
			}
		})
	}
}
