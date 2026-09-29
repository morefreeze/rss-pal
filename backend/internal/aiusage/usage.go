// Package aiusage records provider-reported usage, never prompts or credentials.
package aiusage

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

type Record struct {
	Provider              string
	Model                 string
	Owner                 int
	Known                 bool
	Input, Cached, Output int64
}
type Price struct {
	Input  float64 `json:"input"`
	Cached float64 `json:"cached"`
	Output float64 `json:"output"`
}

// USD per million tokens, verified 2026-09-24 against Z.AI's public price list.
func PriceFor(r Record) (Price, bool) {
	if r.Provider == "api.z.ai" && r.Model == "glm-5.3-flash" {
		return Price{.15, .03, .50}, true
	}
	if r.Provider == "api.z.ai" && r.Model == "glm-5.3" {
		return Price{1.4, .26, 4.4}, true
	}
	return Price{}, false
}
func (p Price) Cost(r Record) float64 {
	return (float64(r.Input-r.Cached)*p.Input + float64(r.Cached)*p.Cached + float64(r.Output)*p.Output) / 1e6
}

type capture struct {
	io.ReadCloser
	stream   bool
	pending  []byte
	overflow bool
	record   Record
	done     func(Record)
	once     sync.Once
}

func Capture(body io.ReadCloser, stream bool, r Record, done func(Record)) io.ReadCloser {
	return &capture{ReadCloser: body, stream: stream, record: r, done: done}
}

// Bound transient memory. Oversized JSON/events are reported as missing usage.
const maxPayload = 8 << 20

func (b *capture) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if !b.overflow {
		b.pending = append(b.pending, p[:n]...)
		if b.stream {
			for {
				i := bytes.IndexByte(b.pending, '\n')
				if i < 0 {
					break
				}
				b.parseLine(b.pending[:i])
				b.pending = b.pending[i+1:]
			}
		}
		if len(b.pending) > maxPayload {
			b.pending = nil
			b.overflow = true
		}
	}
	return n, e
}
func (b *capture) parseLine(line []byte) {
	if bytes.HasPrefix(line, []byte("data:")) {
		b.parse(bytes.TrimSpace(line[5:]))
	}
}
func (b *capture) parse(data []byte) {
	var v struct {
		Model string `json:"model"`
		Usage *struct {
			Input   *int64 `json:"prompt_tokens"`
			Output  *int64 `json:"completion_tokens"`
			Details struct {
				Cached int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &v) != nil || v.Usage == nil || v.Usage.Input == nil || v.Usage.Output == nil {
		return
	}
	u := v.Usage
	if *u.Input < 0 || *u.Output < 0 || u.Details.Cached < 0 || u.Details.Cached > *u.Input {
		return
	}
	b.record.Known = true
	b.record.Input = *u.Input
	b.record.Output = *u.Output
	b.record.Cached = u.Details.Cached
	if v.Model != "" {
		b.record.Model = v.Model
	}
}
func (b *capture) Close() error {
	var err error
	b.once.Do(func() {
		err = b.ReadCloser.Close()
		if !b.overflow {
			if b.stream {
				b.parseLine(b.pending)
			} else {
				b.parse(b.pending)
			}
		}
		b.pending = nil
		b.done(b.record)
	})
	return err
}
