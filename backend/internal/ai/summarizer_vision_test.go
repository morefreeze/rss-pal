package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// captureRequests starts a fake OpenAI-compatible chat server that records
// every inbound request body and replies with a fixed brief/detailed message.
type captured struct {
	bodies [][]byte
}

func newCaptureServer(t *testing.T, replies ...string) (*httptest.Server, *captured) {
	t.Helper()
	cap := &captured{}
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.bodies = append(cap.bodies, body)
		reply := "ok"
		if idx < len(replies) {
			reply = replies[idx]
		}
		idx++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + jsonString(reply) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func writeTestJPEG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 0, G: 255, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write jpeg: %v", err)
	}
}

func TestSummarize_legacyContentShape(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief content", "detailed content")
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")

	_, err := s.Summarize(context.Background(), "Title", "Body text")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(cap.bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(cap.bodies))
	}
	for i, body := range cap.bodies {
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("body %d not json: %v", i, err)
		}
		msgs, _ := parsed["messages"].([]any)
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			c, _ := mm["content"]
			if _, isString := c.(string); !isString {
				t.Errorf("body %d: expected legacy string content, got %T = %v", i, c, c)
			}
		}
	}
}

func TestSummarizeWithImages_visionShape(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief vision", "detailed vision")
	s := NewSummarizerWithModel("test-key", srv.URL, "text-model")
	s.SetVisionModel("vision-model")

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "0.jpg")
	writeTestJPEG(t, imgPath)

	_, err := s.SummarizeWithImages(context.Background(), "Title", "Some text", []string{imgPath})
	if err != nil {
		t.Fatalf("SummarizeWithImages: %v", err)
	}
	if len(cap.bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(cap.bodies))
	}
	for i, body := range cap.bodies {
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("body %d not json: %v", i, err)
		}
		if got := parsed["model"]; got != "vision-model" {
			t.Errorf("body %d: model field want=%q got=%v", i, "vision-model", got)
		}
		msgs, _ := parsed["messages"].([]any)
		userFound := false
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			if mm["role"] != "user" {
				continue
			}
			userFound = true
			arr, isArr := mm["content"].([]any)
			if !isArr {
				t.Errorf("body %d: expected user content as array, got %T", i, mm["content"])
				continue
			}
			if len(arr) < 2 {
				t.Errorf("body %d: expected ≥1 text + ≥1 image block, got %d total", i, len(arr))
			}
			hasText, hasImage := false, false
			for _, blk := range arr {
				bb, _ := blk.(map[string]any)
				switch bb["type"] {
				case "text":
					hasText = true
				case "image_url":
					hasImage = true
					iu, _ := bb["image_url"].(map[string]any)
					url, _ := iu["url"].(string)
					if !startsWith(url, "data:image/jpeg;base64,") {
						t.Errorf("body %d: image_url not base64 data URL: %q", i, truncate(url, 40))
					}
				}
			}
			if !hasText || !hasImage {
				t.Errorf("body %d: hasText=%v hasImage=%v", i, hasText, hasImage)
			}
		}
		if !userFound {
			t.Errorf("body %d: no user message in payload", i)
		}
	}
}

func TestSummarizeWithImages_emptyImageList_fallsBackToTextPath(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief", "detailed")
	s := NewSummarizerWithModel("test-key", srv.URL, "text-model")
	s.SetVisionModel("vision-model")

	_, err := s.SummarizeWithImages(context.Background(), "Title", "Body", nil)
	if err != nil {
		t.Fatalf("SummarizeWithImages with nil images: %v", err)
	}
	if len(cap.bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(cap.bodies))
	}
	var parsed map[string]any
	_ = json.Unmarshal(cap.bodies[0], &parsed)
	if parsed["model"] != "text-model" {
		t.Errorf("with no images, expected text-model fallback, got %v", parsed["model"])
	}
}

func startsWith(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
