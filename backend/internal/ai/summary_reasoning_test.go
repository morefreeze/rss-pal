package ai

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSummaryReasoningEffort(t *testing.T) {
	for _, model := range []string{"glm-5.3", "glm-5.3-flash", "test-model"} {
		for _, mode := range []string{"text", "stream", "vision", "vision-stream", "custom", "custom-stream"} {
			t.Run(model+"/"+mode, func(t *testing.T) {
				stream := strings.Contains(mode, "stream")
				var url string
				var bodies func() [][]byte
				if stream {
					srv, cap := newStreamCaptureServer(t)
					url = srv.URL
					bodies = func() [][]byte { return cap.bodies }
				} else {
					srv, cap := newCaptureServer(t, "brief", "detailed")
					url = srv.URL
					bodies = func() [][]byte { return cap.bodies }
				}
				s := NewSummarizerWithModel("key", url, model)
				s.DisableArticleRouting()
				s.SetVisionModel(model)
				ctx := context.Background()
				var err error
				switch mode {
				case "text":
					_, err = s.Summarize(ctx, "title", "content")
				case "stream":
					_, err = s.SummarizeStream(ctx, "title", "content", func(string) {}, func(string) {})
				case "custom":
					_, err = s.SummarizeWithTemplate(ctx, "title", "content", "{content}", "{content}")
				case "custom-stream":
					_, err = s.SummarizeWithTemplateStream(ctx, "title", "content", "{content}", "{content}", func(string) {}, func(string) {})
				default:
					path := filepath.Join(t.TempDir(), "test.jpg")
					writeTestJPEG(t, path)
					if stream {
						_, err = s.SummarizeWithImagesStream(ctx, "title", "content", []string{path}, nil, func(string) {}, func(string) {})
					} else {
						_, err = s.SummarizeWithImages(ctx, "title", "content", []string{path}, nil)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, body := range bodies() {
					var req map[string]any
					if err := json.Unmarshal(body, &req); err != nil {
						t.Fatal(err)
					}
					want := ""
					if model != "test-model" && !strings.HasPrefix(mode, "custom") {
						want = "low"
					}
					got, _ := req["reasoning_effort"].(string)
					if got != want {
						t.Errorf("reasoning_effort=%q want %q", got, want)
					}
				}
				// Selecting summary reasoning must not leak to a later general AI call.
				before := len(bodies())
				var generalErr error
				if stream {
					_, generalErr = s.callStream(ctx, "general analysis", 4000, func(string) {})
				} else {
					_, generalErr = s.call(ctx, "general analysis", 4000)
				}
				if err := generalErr; err != nil {
					t.Fatal(err)
				}
				var req map[string]any
				json.Unmarshal(bodies()[before], &req)
				if _, ok := req["reasoning_effort"]; ok {
					t.Fatal("summary setting mutated shared client")
				}
			})
		}
	}
}
