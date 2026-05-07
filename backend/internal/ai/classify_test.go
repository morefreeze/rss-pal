package ai

import (
	"reflect"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"topic":"AI","tags":["a"]}`, `{"topic":"AI","tags":["a"]}`},
		{"with fence", "```json\n{\"topic\":\"AI\",\"tags\":[]}\n```", `{"topic":"AI","tags":[]}`},
		{"prefix garbage", "Sure! {\"topic\":\"x\"}", `{"topic":"x"}`},
		{"trailing text", `{"topic":"x","tags":["y"]}  more notes`, `{"topic":"x","tags":["y"]}`},
		{"no braces", `not json at all`, ``},
		{"nested", `{"topic":"x","tags":["a","b"],"extra":{"k":1}}`, `{"topic":"x","tags":["a","b"],"extra":{"k":1}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSON(tc.in)
			if got != tc.want {
				t.Errorf("extractJSON(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseClassification(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantTopic string
		wantTags  []string
		wantErr   bool
	}{
		{"happy", `{"topic":"AI","tags":["OpenAI","GPT-5"]}`, "AI", []string{"OpenAI", "GPT-5"}, false},
		{"with fence", "```\n{\"topic\":\"金融\",\"tags\":[\"FOMC\"]}\n```", "金融", []string{"FOMC"}, false},
		{"empty tags", `{"topic":"编程","tags":[]}`, "编程", []string{}, false},
		{"missing tags ok", `{"topic":"AI"}`, "AI", nil, false},
		{"junk", `not json`, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cls, err := parseClassification(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", cls)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cls.Topic != tc.wantTopic {
				t.Errorf("topic = %q; want %q", cls.Topic, tc.wantTopic)
			}
			if !reflect.DeepEqual(cls.Tags, tc.wantTags) {
				t.Errorf("tags = %v; want %v", cls.Tags, tc.wantTags)
			}
		})
	}
}
