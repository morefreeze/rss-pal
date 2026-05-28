package ai

import (
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
)

func TestShouldUseVisionAuto(t *testing.T) {
	defaultCfg := config.VisionConfig{
		MinImages:    3,
		MaxTextChars: 2000,
	}
	cases := []struct {
		name    string
		content string
		cfg     config.VisionConfig
		want    bool
	}{
		{"empty content", "", defaultCfg, false},
		{"3 images, zero text", "![](https://a.com/1.png)![](https://a.com/2.png)![](https://a.com/3.png)", defaultCfg, true},
		{"2 images (below MinImages)", "![](https://a.com/1.png)![](https://a.com/2.png)", defaultCfg, false},
		{"3 images but too much text",
			"![](https://a.com/1.png)![](https://a.com/2.png)![](https://a.com/3.png)" +
				strings.Repeat("text ", 500), // 2500 chars of text
			defaultCfg, false},
		{"5 images, short text", "![](a)![](b)![](c)![](d)![](e)\n短正文", defaultCfg, true},
		{"text exactly at the boundary still triggers (<2000 chars)",
			"![](a)![](b)![](c)" + strings.Repeat("x", 1999), defaultCfg, true},
		{"text just over boundary fails",
			"![](a)![](b)![](c)" + strings.Repeat("x", 2000), defaultCfg, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldUseVisionAuto(tc.content, tc.cfg)
			if got != tc.want {
				t.Fatalf("want=%v got=%v", tc.want, got)
			}
		})
	}
}
