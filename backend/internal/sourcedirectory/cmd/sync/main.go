// Command sync is used by scripts/sync-source-directory.py to ensure the checked
// in snapshot and live refresh share exactly one normalization implementation.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bytedance/rss-pal/internal/sourcedirectory"
)

func main() {
	fetched := flag.String("fetched-at", time.Now().UTC().Format(time.RFC3339), "upstream fetch time (RFC3339)")
	flag.Parse()
	timestamp, err := time.Parse(time.RFC3339, *fetched)
	if err != nil {
		fail(err)
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, (20<<20)+1))
	if err != nil {
		fail(err)
	}
	if len(data) > 20<<20 {
		fail(fmt.Errorf("directory exceeds 20 MiB"))
	}
	d, err := sourcedirectory.Normalize(data, timestamp)
	if err != nil {
		fail(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(d); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
