package ai

import (
	"strings"
	"testing"
)

func TestParseInsightJSON_HappyPath(t *testing.T) {
	raw := `{
		"markdown": "## 核心兴趣\n你喜欢分布式系统",
		"recommendations": [
			{
				"direction": "分布式系统",
				"direction_kind": "core",
				"articles": [
					{"article_id": 1, "reason": "深度讨论一致性"},
					{"article_id": 2, "reason": "Raft 算法解析"}
				]
			}
		]
	}`
	pool := map[int]bool{1: true, 2: true, 3: true}
	md, recs, dropped := ParseInsightJSON(raw, pool)
	if !strings.Contains(md, "核心兴趣") {
		t.Errorf("markdown lost: %q", md)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 direction, got %d", len(recs))
	}
	if recs[0].DirectionKind != "core" || len(recs[0].Articles) != 2 {
		t.Errorf("direction wrong: %+v", recs[0])
	}
	if len(dropped) != 0 {
		t.Errorf("did not expect drops, got %v", dropped)
	}
}

func TestParseInsightJSON_FenceWrapped(t *testing.T) {
	raw := "```json\n{\"markdown\":\"hi\",\"recommendations\":[]}\n```"
	md, recs, _ := ParseInsightJSON(raw, nil)
	if md != "hi" {
		t.Errorf("markdown = %q", md)
	}
	if len(recs) != 0 {
		t.Errorf("recs should be empty: %+v", recs)
	}
}

func TestParseInsightJSON_DropsInvalidIDsAndKinds(t *testing.T) {
	raw := `{
		"markdown": "ok",
		"recommendations": [
			{"direction": "A", "direction_kind": "core", "articles": [
				{"article_id": 1, "reason": "valid"},
				{"article_id": 999, "reason": "fake id"}
			]},
			{"direction": "B", "direction_kind": "weird", "articles": [
				{"article_id": 1, "reason": "kind not allowed"}
			]},
			{"direction": "", "direction_kind": "emerging", "articles": [
				{"article_id": 1, "reason": "empty direction"}
			]},
			{"direction": "C", "direction_kind": "emerging", "articles": [
				{"article_id": 1, "reason": ""}
			]}
		]
	}`
	pool := map[int]bool{1: true, 2: true}
	md, recs, dropped := ParseInsightJSON(raw, pool)
	if md != "ok" {
		t.Errorf("md = %q", md)
	}
	if len(recs) != 1 {
		t.Fatalf("want only the first direction, got %d: %+v", len(recs), recs)
	}
	if len(recs[0].Articles) != 1 || recs[0].Articles[0].ArticleID != 1 {
		t.Errorf("survivor wrong: %+v", recs[0])
	}
	if len(dropped) == 0 {
		t.Errorf("expected drop reasons logged, got none")
	}
}

func TestParseInsightJSON_TotalGarbage(t *testing.T) {
	md, recs, dropped := ParseInsightJSON("not json at all", map[int]bool{})
	if md != "not json at all" {
		t.Errorf("md should fall back to raw: %q", md)
	}
	if len(recs) != 0 {
		t.Errorf("recs should be empty: %+v", recs)
	}
	if len(dropped) == 0 {
		t.Errorf("should record a drop reason for unparseable input")
	}
}

func TestParseInsightJSON_CapsAt3DirectionsAnd5Articles(t *testing.T) {
	raw := `{
		"markdown": "x",
		"recommendations": [
			{"direction":"A","direction_kind":"core","articles":[{"article_id":1,"reason":"r"}]},
			{"direction":"B","direction_kind":"core","articles":[{"article_id":2,"reason":"r"}]},
			{"direction":"C","direction_kind":"core","articles":[{"article_id":3,"reason":"r"}]},
			{"direction":"D","direction_kind":"emerging","articles":[{"article_id":4,"reason":"r"}]}
		]
	}`
	pool := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true}
	_, recs, _ := ParseInsightJSON(raw, pool)
	if len(recs) != 3 {
		t.Errorf("expected 3-direction cap, got %d", len(recs))
	}

	raw2 := `{
		"markdown": "x",
		"recommendations": [
			{"direction":"A","direction_kind":"core","articles":[
				{"article_id":1,"reason":"r"},
				{"article_id":2,"reason":"r"},
				{"article_id":3,"reason":"r"},
				{"article_id":4,"reason":"r"},
				{"article_id":5,"reason":"r"},
				{"article_id":6,"reason":"r"}
			]}
		]
	}`
	_, recs2, _ := ParseInsightJSON(raw2, pool)
	total := 0
	for _, d := range recs2 {
		total += len(d.Articles)
	}
	if total != 5 {
		t.Errorf("expected 5-article cap, got %d", total)
	}
}
