package config

import "testing"

func TestLoadShareSecret(t *testing.T) {
	t.Setenv("SHARE_SECRET", "share-secret-for-test")
	t.Setenv("SHORT_SHARE_ORIGIN", "https://r.morefreeze.top")

	cfg := Load()
	if got := cfg.Share.Secret; got != "share-secret-for-test" {
		t.Fatalf("Load().Share.Secret = %q, want %q", got, "share-secret-for-test")
	}
	if got := cfg.Share.ShortOrigin; got != "https://r.morefreeze.top" {
		t.Fatalf("Load().Share.ShortOrigin = %q, want %q", got, "https://r.morefreeze.top")
	}
}

func TestValidateShortShareOriginAcceptsExactProductionOrigin(t *testing.T) {
	const raw = "https://r.morefreeze.top"
	got, err := ValidateShortShareOrigin(raw)
	if err != nil {
		t.Fatalf("ValidateShortShareOrigin(%q) error = %v", raw, err)
	}
	if got != raw {
		t.Fatalf("ValidateShortShareOrigin(%q) = %q, want unchanged origin", raw, got)
	}
}

func TestValidateShortShareOriginRejectsAnythingExceptExactProductionOrigin(t *testing.T) {
	const wantErr = "SHORT_SHARE_ORIGIN must be exactly https://r.morefreeze.top"
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "http", raw: "http://r.morefreeze.top"},
		{name: "custom HTTPS origin", raw: "https://short.example.test"},
		{name: "explicit port", raw: "https://r.morefreeze.top:443"},
		{name: "hostname casing", raw: "https://R.morefreeze.top"},
		{name: "trailing dot", raw: "https://r.morefreeze.top."},
		{name: "trailing slash", raw: "https://r.morefreeze.top/"},
		{name: "path", raw: "https://r.morefreeze.top/share"},
		{name: "credentials", raw: "https://user:pass@r.morefreeze.top"},
		{name: "query", raw: "https://r.morefreeze.top?source=rss"},
		{name: "force query", raw: "https://r.morefreeze.top?"},
		{name: "fragment", raw: "https://r.morefreeze.top#share"},
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
