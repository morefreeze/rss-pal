package config

import "testing"

func TestLoadShareSecret(t *testing.T) {
	t.Setenv("SHARE_SECRET", "share-secret-for-test")

	if got := Load().Share.Secret; got != "share-secret-for-test" {
		t.Fatalf("Load().Share.Secret = %q, want %q", got, "share-secret-for-test")
	}
}
