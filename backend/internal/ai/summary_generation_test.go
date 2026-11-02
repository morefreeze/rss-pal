package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummaryGenerationUsesAcceptedWireRequestAfterTokenRetry(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		reason := "stop"
		if len(bodies) == 1 {
			reason = "length"
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"` + reason + `","message":{"content":"result"}}]}`))
	}))
	defer server.Close()
	s := NewSummarizerWithModel("test-key", server.URL, "model")
	result, err := s.Summarize(context.Background(), "Title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 || result.Generation.Stages[0].RequestHash != summaryHash(string(bodies[1])) {
		t.Fatalf("wrong retry provenance: %+v", result.Generation)
	}
}

func TestSummaryGenerationHashesNativeAnthropicPayload(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"type":"message","model":"claude","content":[{"type":"text","text":"result"}],"stop_reason":"end_turn"}`))
	}))
	defer server.Close()
	s := NewSummarizerWithModel("test-key", server.URL, "claude")
	s.providerProtocol = "anthropic"
	result, err := s.Summarize(context.Background(), "Title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || result.Generation.Stages[0].RequestHash != summaryHash(string(bodies[0])) {
		t.Fatalf("wrong native provenance: %+v", result.Generation)
	}
}

func TestSummaryGenerationTracksEveryProviderRoute(t *testing.T) {
	for _, route := range []string{"default", "stream", "custom", "custom-stream", "vision", "vision-stream"} {
		t.Run(route, func(t *testing.T) {
			var url string
			var bodies func() [][]byte
			if strings.Contains(route, "stream") {
				srv, cap := newStreamCaptureServer(t, "brief", "details")
				url, bodies = srv.URL, func() [][]byte { return cap.bodies }
			} else {
				srv, cap := newCaptureServer(t, "brief", "details")
				url, bodies = srv.URL, func() [][]byte { return cap.bodies }
			}
			s := NewSummarizerWithModel("test-key", url, "text-model")
			s.SetVisionModel("vision-model")
			path := filepath.Join(t.TempDir(), "image.jpg")
			writeTestJPEG(t, path)
			source := strings.Repeat("source body with final conditions. ", 500)
			cb := func(string) {}
			var result *SummaryResult
			var err error
			switch route {
			case "default":
				result, err = s.Summarize(context.Background(), "Title", source)
			case "stream":
				result, err = s.SummarizeStream(context.Background(), "Title", source, cb, cb)
			case "custom":
				result, err = s.SummarizeWithTemplate(context.Background(), "Title", source, "Brief {content}", "Details {content}")
			case "custom-stream":
				result, err = s.SummarizeWithTemplateStream(context.Background(), "Title", source, "Brief {content}", "Details {content}", cb, cb)
			case "vision":
				result, err = s.SummarizeWithImages(context.Background(), "Title", source, []string{path}, nil)
			case "vision-stream":
				result, err = s.SummarizeWithImagesStream(context.Background(), "Title", source, []string{path}, nil, cb, cb)
			}
			if err != nil {
				t.Fatal(err)
			}
			g := result.Generation
			if g == nil || g.PolicyVersion != SummaryPolicyVersion || g.ContentHash != summaryHash(source) || g.InputHash != summaryHash("Title\x00"+source) || g.GeneratedAt.IsZero() {
				t.Fatalf("missing provenance: %+v", g)
			}
			if len(g.Stages) != 2 {
				t.Fatalf("stages: %+v", g.Stages)
			}
			for i, body := range bodies() {
				var req struct {
					Model    string          `json:"model"`
					Messages json.RawMessage `json:"messages"`
				}
				if err := json.Unmarshal(body, &req); err != nil {
					t.Fatal(err)
				}
				stage := g.Stages[i]
				if stage.Model != req.Model || stage.ModelSource != "server-requested" || stage.RequestHash != summaryHash(string(body)) || stage.PromptHash != summaryHash(string(req.Messages)) {
					t.Fatalf("stage mismatch: %+v", stage)
				}
				system, _ := summaryRequestMessages(t, body)
				for _, required := range []string{"收入类别", "生命周期", "排除的对象"} {
					if !strings.Contains(system, required) {
						t.Fatalf("missing general policy %q", required)
					}
				}
			}
			if strings.Contains(route, "custom") && g.TemplateHash != summaryHash("Brief {content}\x00Details {content}") {
				t.Fatalf("template not tracked: %+v", g)
			}
			if s.generation != nil {
				t.Fatal("shared summarizer retained per-request mutable state")
			}
		})
	}
}

func TestLocalSummaryGenerationBindingDetectsPolicyTemplateAndTitleChanges(t *testing.T) {
	g := LocalSummaryGeneration("Title", "body", "", "")
	if g.PolicyVersion != SummaryPolicyVersion || g.InputHash != summaryHash("Title\x00body") || g.Execution != "client-reported" {
		t.Fatalf("local %+v", g)
	}
	for _, other := range []string{LocalSummaryGeneration("Changed", "body", "", "").BindingHash, LocalSummaryGeneration("Title", "changed", "", "").BindingHash, LocalSummaryGeneration("Title", "body", "custom", "details").BindingHash} {
		if other == g.BindingHash {
			t.Fatal("binding does not distinguish inputs")
		}
	}
	b, d := LocalSummaryPrompts("Title", "body", "", "")
	for _, prompt := range []string{b, d} {
		if !strings.Contains(prompt, "生命周期") || !strings.Contains(prompt, "排除的对象") {
			t.Fatal("local policy differs")
		}
	}
}
