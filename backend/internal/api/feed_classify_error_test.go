package api

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyPreviewError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantSub string // substring the message must contain
	}{
		{"429 rate limit", errors.New("server returned 429"), "限流"},
		{"503 unavailable", errors.New("server returned 503"), "暂时不可用"},
		{"403 forbidden", errors.New("server returned 403"), "拒绝访问"},
		{"404 not found", errors.New("server returned 404"), "不存在"},
		{"500 generic 5xx", errors.New("server returned 500"), "源站异常"},
		{"502 bad gateway 5xx", errors.New("server returned 502"), "源站异常"},
		{"timeout", errors.New("failed to fetch URL: Get \"https://x.com\": context deadline exceeded"), "超时"},
		{"dns failure", errors.New("failed to fetch URL: Get \"https://nope.invalid\": dial tcp: lookup nope.invalid: no such host"), "解析域名"},
		{"connection refused", errors.New("failed to fetch URL: Get \"https://x\": dial tcp 1.1.1.1:443: connect: connection refused"), "无法连接"},
		{"parse failure", errors.New("failed to parse page: html parsing failed"), "不是有效的 RSS"},
		{"unknown", errors.New("something weird happened"), "无法获取"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyPreviewError(tt.err)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("classifyPreviewError(%q) = %q, want substring %q", tt.err.Error(), got, tt.wantSub)
			}
		})
	}
}
