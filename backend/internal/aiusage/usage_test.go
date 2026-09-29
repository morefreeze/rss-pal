package aiusage

import (
	"io"
	"math"
	"strings"
	"testing"
)

func TestCaptureUsage(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		stream                bool
		known                 bool
		input, cached, output int64
	}{
		{"json", `{"model":"glm-5.3-flash","usage":{"prompt_tokens":1000,"completion_tokens":200,"prompt_tokens_details":{"cached_tokens":400}}}`, false, true, 1000, 400, 200},
		{"sse", "data: {\"choices\":[],\"usage\":null}\n\ndata: {\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":200,\"prompt_tokens_details\":{\"cached_tokens\":400}}}\n\ndata: [DONE]\n", true, true, 1000, 400, 200},
		{"missing", `{"choices":[]}`, false, false, 0, 0, 0},
		{"invalid", `{"usage":{"prompt_tokens":1,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":3}}}`, false, false, 0, 0, 0},
		{"partial", `{"usage":{"prompt_tokens":12}}`, false, false, 0, 0, 0},
		{"zero", `{"usage":{"prompt_tokens":0,"completion_tokens":0}}`, false, true, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got Record
			calls := 0
			b := Capture(io.NopCloser(strings.NewReader(tc.body)), tc.stream, Record{Model: "glm-5.3-flash", Provider: "api.z.ai"}, func(r Record) { got = r; calls++ })
			raw, e := io.ReadAll(b)
			if e != nil || string(raw) != tc.body {
				t.Fatal("response changed", e)
			}
			b.Close()
			b.Close()
			if calls != 1 || got.Known != tc.known || got.Input != tc.input || got.Cached != tc.cached || got.Output != tc.output {
				t.Fatalf("calls=%d %+v", calls, got)
			}
		})
	}
}
func TestPricesAndCachedInput(t *testing.T) {
	r := Record{Provider: "api.z.ai", Model: "glm-5.3-flash", Known: true, Input: 1000000, Cached: 400000, Output: 200000}
	p, ok := PriceFor(r)
	if !ok {
		t.Fatal("missing default price")
	}
	if math.Abs(p.Cost(r)-.202) > 1e-10 {
		t.Fatalf("double-counted cached input: %v", p.Cost(r))
	}
	for _, r := range []Record{{Provider: "other.example", Model: "glm-5.3-flash"}, {Provider: "api.z.ai", Model: "unknown"}} {
		if _, ok := PriceFor(r); ok {
			t.Fatal("guessed price", r)
		}
	}
}

func TestFlagshipPrice(t *testing.T) {
	r := Record{Provider: "api.z.ai", Model: "glm-5.3", Known: true, Input: 1000000, Cached: 400000, Output: 200000}
	p, ok := PriceFor(r)
	if !ok || math.Abs(p.Cost(r)-1.824) > 1e-10 {
		t.Fatal("flagship rate", p, ok)
	}
}
