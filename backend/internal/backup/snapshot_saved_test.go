package backup

import "testing"

func TestSavedSiblingPath(t *testing.T) {
	cases := []struct {
		metadata, want string
	}{
		{
			metadata: "/tmp/backups/rss-pal-backup-20260514-093015.json",
			want:     "/tmp/backups/rss-pal-backup-20260514-093015.saved.json.gz",
		},
		{
			metadata: "rss-pal-backup-20260101-000000.json",
			want:     "rss-pal-backup-20260101-000000.saved.json.gz",
		},
		{
			metadata: "/tmp/x/y/foo.json",
			want:     "/tmp/x/y/foo.saved.json.gz",
		},
		{
			metadata: "rss-pal-backup-20260514", // no .json suffix → fallback path
			want:     "rss-pal-backup-20260514.saved.json.gz",
		},
	}
	for _, c := range cases {
		got := savedSiblingPath(c.metadata)
		if got != c.want {
			t.Errorf("savedSiblingPath(%q) = %q, want %q", c.metadata, got, c.want)
		}
	}
}
