package aiusage

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"math"
	"testing"
	"time"
)

func TestStoredCostWindowsAndMissing(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	save := Recorder(db)
	save(Record{Provider: "api.z.ai", Model: "glm-5.3-flash", Known: true, Input: 1000000, Cached: 400000, Output: 200000})
	save(Record{Provider: "api.z.ai", Model: "glm-5.3-flash"})
	save(Record{Provider: "other.example", Model: "glm-5.3-flash", Known: true, Input: 100, Output: 20})
	_, e := db.Exec(`INSERT INTO ai_token_usage(at,provider,model,cost_usd) VALUES(now()-interval '2 hours','old','old',99)`)
	if e != nil {
		t.Fatal(e)
	}
	s, e := Snapshot(context.Background(), db, time.Now().Add(-time.Hour), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Models) != 2 {
		t.Fatalf("window: %+v", s)
	}
	m := s.Models[0]
	if m.Calls != 2 || m.Missing != 1 || m.Cached != 400000 || m.Cost == nil || math.Abs(*m.Cost-.202) > 1e-9 {
		t.Fatalf("aggregation: %+v", m)
	}
	if s.Models[1].Unpriced != 1 || s.Models[1].Cost != nil {
		t.Fatal("unknown model billed")
	}
}
