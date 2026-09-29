package feedcatalog

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCatalogCheckErrorsNeverExposeTransportDetails(t *testing.T) {
	for _, err := range []error{errors.New("proxy https://user:SECRET@proxy.invalid failed"), errors.New("RSS HTTP status 403"), context.DeadlineExceeded, errors.New("RSS parse failed: bad RSS")} {
		got := catalogCheckFailure(err)
		if got == "" || strings.Contains(got, "SECRET") || strings.Contains(got, "proxy.invalid") {
			t.Fatalf("unsafe %s", got)
		}
	}
	if got := catalogCheckFailure(errors.New("RSS HTTP status 403")); !strings.Contains(got, "403") {
		t.Fatal(got)
	}
}

func TestCatalogURLPreservesOpaqueQuery(t *testing.T) {
	for _, raw := range []string{"https://example.com/feed?filter=a;b&sig=A%2FB", "https://example.com/feed?z=2&a=1&sig=abc%20def"} {
		got, err := normalizeCatalogURL(raw)
		if err != nil || got != raw {
			t.Fatalf("query changed %q => %q, %v", raw, got, err)
		}
	}
}
