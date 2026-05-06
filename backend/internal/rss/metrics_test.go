package rss

import "testing"

func TestComputeMetrics_PureChinese(t *testing.T) {
	// 600 Han chars -> word_count=600, reading_minutes=2 (600/300=2)
	text := ""
	for i := 0; i < 600; i++ {
		text += "中"
	}
	wc, rm := ComputeMetrics(text)
	if wc != 600 {
		t.Errorf("word_count = %d, want 600", wc)
	}
	if rm != 2 {
		t.Errorf("reading_minutes = %d, want 2", rm)
	}
}

func TestComputeMetrics_PureEnglish(t *testing.T) {
	// 500 English words -> wc=500, rm=2 (500/250=2)
	text := ""
	for i := 0; i < 500; i++ {
		text += "word "
	}
	wc, rm := ComputeMetrics(text)
	if wc != 500 {
		t.Errorf("word_count = %d, want 500", wc)
	}
	if rm != 2 {
		t.Errorf("reading_minutes = %d, want 2", rm)
	}
}

func TestComputeMetrics_Mixed(t *testing.T) {
	// 300 zh chars + 250 en words -> wc=550, rm=max(1, round(300/300 + 250/250))=2
	text := ""
	for i := 0; i < 300; i++ {
		text += "中"
	}
	for i := 0; i < 250; i++ {
		text += " word"
	}
	wc, rm := ComputeMetrics(text)
	if wc != 550 {
		t.Errorf("word_count = %d, want 550", wc)
	}
	if rm != 2 {
		t.Errorf("reading_minutes = %d, want 2", rm)
	}
}

func TestComputeMetrics_Empty(t *testing.T) {
	wc, rm := ComputeMetrics("")
	if wc != 0 {
		t.Errorf("word_count = %d, want 0", wc)
	}
	if rm != 0 {
		t.Errorf("reading_minutes = %d, want 0", rm)
	}
}

func TestComputeMetrics_VeryShort(t *testing.T) {
	// 50 zh chars -> rm = max(1, round(50/300)) = max(1, 0) = 1 (only when wc>0)
	text := ""
	for i := 0; i < 50; i++ {
		text += "中"
	}
	wc, rm := ComputeMetrics(text)
	if wc != 50 {
		t.Errorf("word_count = %d, want 50", wc)
	}
	if rm != 1 {
		t.Errorf("reading_minutes = %d, want 1 (floor minimum)", rm)
	}
}

func TestComputeMetrics_StripsHTMLTags(t *testing.T) {
	// HTML tags should be ignored; only visible text counted.
	text := "<p>Hello <strong>world</strong></p>"
	wc, _ := ComputeMetrics(text)
	if wc != 2 {
		t.Errorf("word_count = %d, want 2 (Hello + world)", wc)
	}
}
