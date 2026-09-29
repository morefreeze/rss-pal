package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bytedance/rss-pal/internal/aiusage"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamingRequestsUsageAndRecordsOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		opts, _ := req["stream_options"].(map[string]any)
		if opts["include_usage"] != true {
			t.Error("usage not requested")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n")
	}))
	defer srv.Close()
	s := NewSummarizerWithModel("unused", srv.URL, "glm-5.3-flash")
	s.SetAdmission(nil)
	var records []aiusage.Record
	s.usageRecorder = func(r aiusage.Record) { records = append(records, r) }
	out, err := s.callStream(context.Background(), "prompt", 100, func(string) {})
	if err != nil || out != "hello" || len(records) != 1 || records[0].Input != 10 || records[0].Output != 5 {
		t.Fatalf("%s %v %+v", out, err, records)
	}
}
