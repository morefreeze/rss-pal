package rss

import (
	"os"
	"strings"
	"testing"
)

func TestFetchContentFromReaderCloudflareTables(t *testing.T) {
	f, err := os.Open("testdata/cloudflare_tables.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := NewContentFetcher().FetchContentFromReader(f)
	if err != nil {
		t.Fatal(err)
	}
	// Real publisher fixture: multiline headers and row-spanning labels must
	// retain their column relationships after conversion to GFM.
	for _, row := range []string{
		"| (Legacy) “Block AI” setting | (New) Search setting | (New) Training setting | (New) Agent setting |",
		"| Disabled (unselected) | Allow | Allow | Allow |",
		"| Control | Legacy setting | New setting |",
		"| Training | Block | **Disallow AI Training** |",
		"| Training | Block on pages with ads | **Disallow AI Training** |",
		"| Setting | Site does not monetize using ads | Site is monetized using ads |",
	} {
		if !strings.Contains(got, row) {
			t.Errorf("missing table row %q in:\n%s", row, got)
		}
	}
}
