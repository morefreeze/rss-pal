package autotag

import "testing"

func TestResolvePrefersExistingTagAboveThreshold(t *testing.T) {
	got := Resolve([]string{"Open AI", "LLM"}, []string{"OpenAI", "Go"}, Options{
		SimilarityThreshold: 0.8,
		MaxTags:             3,
	})

	if len(got) != 2 {
		t.Fatalf("Resolve returned %d tags, want 2: %#v", len(got), got)
	}
	if got[0].Name != "OpenAI" || got[0].Action != ReuseExisting {
		t.Fatalf("first tag = %#v, want reuse of existing OpenAI", got[0])
	}
	if got[0].Similarity < 0.8 {
		t.Fatalf("similarity = %.3f, want >= 0.8", got[0].Similarity)
	}
	if got[1].Name != "LLM" || got[1].Action != CreateNew {
		t.Fatalf("second tag = %#v, want create of LLM", got[1])
	}
}

func TestResolveCreatesUsefulTagsForColdStart(t *testing.T) {
	got := Resolve([]string{"AI", "文章", "向量数据库", "RAG", "RAG"}, nil, Options{
		SimilarityThreshold: 0.8,
		MaxTags:             3,
	})

	names := namesOf(got)
	want := []string{"向量数据库", "RAG"}
	if len(names) != len(want) {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %#v, want %#v", names, want)
		}
	}
	for _, d := range got {
		if d.Action != CreateNew {
			t.Fatalf("decision = %#v, want CreateNew for cold-start tags", d)
		}
	}
}

func TestResolveDeduplicatesAfterCanonicalization(t *testing.T) {
	got := Resolve([]string{"OpenAI", "open ai", "OpenAI 公司"}, []string{"OpenAI"}, Options{
		SimilarityThreshold: 0.8,
		MaxTags:             3,
	})

	if len(got) != 1 {
		t.Fatalf("Resolve returned %#v, want one deduplicated OpenAI binding", got)
	}
	if got[0].Name != "OpenAI" || got[0].Action != ReuseExisting {
		t.Fatalf("decision = %#v, want reuse of OpenAI", got[0])
	}
}

func namesOf(decisions []Decision) []string {
	out := make([]string, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, d.Name)
	}
	return out
}
