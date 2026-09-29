package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

// Bound request, response and individual SSE event buffers. The adapter is a
// pull reader: closing a partially consumed stream cannot leave a goroutine.
const anthropicPayloadLimit = 32 << 20

// Retain the retry classification, but never wrap or retain the provider error:
// its text can contain request URLs or credentials echoed by an upstream.
type anthropicNetworkError struct {
	message   string
	transient bool
}

func (e *anthropicNetworkError) Error() string   { return e.message }
func (e *anthropicNetworkError) Transient() bool { return e.transient }
func safeAnthropicError(message string, cause error) error {
	return &anthropicNetworkError{message: message, transient: isTransientNetErr(cause)}
}

func readAnthropicPayload(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, anthropicPayloadLimit+1))
	if err != nil {
		return nil, safeAnthropicError("anthropic response read failed", err)
	}
	if len(b) > anthropicPayloadLimit {
		return nil, errors.New("anthropic payload too large")
	}
	return b, nil
}

func anthropicContent(raw json.RawMessage) (interface{}, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return nil, errors.New("unsupported anthropic message content")
	}
	out := make([]interface{}, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, map[string]interface{}{"type": "text", "text": b.Text})
		case "image_url":
			if b.ImageURL == nil {
				return nil, errors.New("missing anthropic image source")
			}
			u := b.ImageURL.URL
			var source map[string]interface{}
			if strings.HasPrefix(u, "data:") {
				header, data, ok := strings.Cut(strings.TrimPrefix(u, "data:"), ",")
				media, base64 := strings.CutSuffix(header, ";base64")
				if !ok || !base64 || !strings.HasPrefix(media, "image/") || data == "" {
					return nil, errors.New("invalid anthropic image data")
				}
				source = map[string]interface{}{"type": "base64", "media_type": media, "data": data}
			} else if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
				source = map[string]interface{}{"type": "url", "url": u}
			} else {
				return nil, errors.New("unsupported anthropic image URL")
			}
			out = append(out, map[string]interface{}{"type": "image", "source": source})
		default:
			return nil, errors.New("unsupported anthropic content block")
		}
	}
	return out, nil
}

func (s *Summarizer) doAnthropic(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return nil, errors.New("missing anthropic request body")
	}
	raw, err := readAnthropicPayload(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}
	var input struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Model == "" || input.MaxTokens <= 0 {
		return nil, errors.New("invalid anthropic request")
	}
	messages := make([]interface{}, 0, len(input.Messages))
	system := make([]interface{}, 0)
	for _, m := range input.Messages {
		c, e := anthropicContent(m.Content)
		if e != nil {
			return nil, e
		}
		if m.Role == "system" {
			switch v := c.(type) {
			case string:
				system = append(system, map[string]interface{}{"type": "text", "text": v})
			case []interface{}:
				for _, b := range v {
					if b.(map[string]interface{})["type"] != "text" {
						return nil, errors.New("unsupported anthropic system content")
					}
					system = append(system, b)
				}
			}
		} else {
			if m.Role != "user" && m.Role != "assistant" {
				return nil, errors.New("unsupported anthropic message role")
			}
			messages = append(messages, map[string]interface{}{"role": m.Role, "content": c})
		}
	}
	if len(messages) == 0 {
		return nil, errors.New("missing anthropic messages")
	}
	body := map[string]interface{}{"model": input.Model, "max_tokens": input.MaxTokens, "stream": input.Stream, "messages": messages}
	if len(system) > 0 {
		body["system"] = system
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, errors.New("invalid anthropic request content")
	}
	native := req.Clone(req.Context())
	urlCopy := *req.URL
	urlCopy.Path = strings.TrimSuffix(urlCopy.Path, "/chat/completions") + "/messages"
	urlCopy.RawPath = ""
	native.URL = &urlCopy
	native.Body = io.NopCloser(bytes.NewReader(payload))
	native.ContentLength = int64(len(payload))
	native.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
	native.Header.Del("Authorization")
	native.Header.Set("x-api-key", s.apiKey)
	native.Header.Set("anthropic-version", "2023-06-01")
	native.Header.Set("Content-Type", "application/json")
	// Go does not treat x-api-key as sensitive when redirecting. Never send
	// native credentials to a redirect target, including another port.
	client := *s.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(native)
	if err != nil {
		return nil, safeAnthropicError("anthropic request failed", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		response.Body = io.NopCloser(strings.NewReader(fmt.Sprintf("anthropic API error (HTTP %d)", response.StatusCode)))
		response.Header.Set("Content-Type", "text/plain")
		response.ContentLength = -1
		return response, nil
	}
	response.ContentLength = -1
	if input.Stream {
		response.Header.Set("Content-Type", "text/event-stream")
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), anthropicPayloadLimit)
		response.Body = &anthropicStream{source: response.Body, scanner: scanner}
		return response, nil
	}
	data, err := readAnthropicPayload(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	var msg anthropicMessage
	if json.Unmarshal(data, &msg) != nil || msg.Type != "message" {
		return nil, errors.New("invalid anthropic response")
	}
	var text strings.Builder
	for _, b := range msg.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	out := map[string]interface{}{"model": msg.Model, "choices": []interface{}{map[string]interface{}{"message": map[string]string{"content": text.String()}}}}
	if usage := msg.Usage.openAI(); usage != nil {
		out["usage"] = usage
	}
	converted, _ := json.Marshal(out)
	response.Body = io.NopCloser(bytes.NewReader(converted))
	response.Header.Set("Content-Type", "application/json")
	return response, nil
}

type anthropicUsage struct {
	Input    *int64 `json:"input_tokens"`
	Output   *int64 `json:"output_tokens"`
	Creation *int64 `json:"cache_creation_input_tokens"`
	Cached   *int64 `json:"cache_read_input_tokens"`
}

func (u anthropicUsage) openAI() interface{} {
	var creation, cached int64
	if u.Creation != nil {
		creation = *u.Creation
	}
	if u.Cached != nil {
		cached = *u.Cached
	}
	if u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output < 0 || creation < 0 || cached < 0 {
		return nil
	}
	if creation > math.MaxInt64-*u.Input || cached > math.MaxInt64-*u.Input-creation {
		return nil
	}
	return map[string]interface{}{"prompt_tokens": *u.Input + creation + cached, "completion_tokens": *u.Output, "prompt_tokens_details": map[string]int64{"cached_tokens": cached}}
}

type anthropicMessage struct {
	Type    string `json:"type"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage anthropicUsage `json:"usage"`
}
type anthropicStream struct {
	source  io.ReadCloser
	scanner *bufio.Scanner
	pending []byte
	done    bool
	started bool
	delta   bool
	model   string
	usage   anthropicUsage
}

func (s *anthropicStream) Close() error { return s.source.Close() }
func (s *anthropicStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(s.pending) == 0 {
		if s.done {
			return 0, io.EOF
		}
		if err := s.next(); err != nil {
			s.done = true
			return 0, err
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}
func (s *anthropicStream) emit(v interface{}) {
	b, _ := json.Marshal(v)
	s.pending = append(s.pending, []byte("data: ")...)
	s.pending = append(s.pending, b...)
	s.pending = append(s.pending, '\n', '\n')
}
func (s *anthropicStream) next() error {
	var data strings.Builder
	for {
		if !s.scanner.Scan() {
			return errors.New("anthropic stream ended before message_stop")
		}
		line := s.scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			if data.Len() > anthropicPayloadLimit {
				return errors.New("anthropic stream event too large")
			}
		}
	}
	var event struct {
		Type    string           `json:"type"`
		Message anthropicMessage `json:"message"`
		Usage   anthropicUsage   `json:"usage"`
		Delta   struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content_block"`
	}
	if json.Unmarshal([]byte(data.String()), &event) != nil || event.Type == "" {
		return errors.New("invalid anthropic stream event")
	}
	switch event.Type {
	case "error":
		return errors.New("anthropic stream provider error")
	case "message_start":
		if s.started {
			return errors.New("duplicate anthropic message_start")
		}
		s.started = true
		s.model = event.Message.Model
		s.usage = event.Message.Usage
		s.usage.Output = nil
	case "message_delta":
		if !s.started {
			return errors.New("anthropic message_delta before message_start")
		}
		s.delta = true
		// Usage in message_delta is cumulative. Update only fields supplied by
		// this event; omitted fields retain the preceding provider counters.
		if event.Usage.Input != nil {
			s.usage.Input = event.Usage.Input
		}
		if event.Usage.Creation != nil {
			s.usage.Creation = event.Usage.Creation
		}
		if event.Usage.Cached != nil {
			s.usage.Cached = event.Usage.Cached
		}
		if event.Usage.Output != nil {
			s.usage.Output = event.Usage.Output
		}
	case "content_block_start", "content_block_delta":
		if !s.started {
			return errors.New("anthropic content before message_start")
		}
		text := ""
		if event.Type == "content_block_start" && event.Content.Type == "text" {
			text = event.Content.Text
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			text = event.Delta.Text
		}
		if text != "" {
			s.emit(map[string]interface{}{"model": s.model, "choices": []interface{}{map[string]interface{}{"delta": map[string]string{"content": text}}}})
		}
	case "message_stop":
		if !s.started || !s.delta {
			return errors.New("incomplete anthropic message")
		}
		if u := s.usage.openAI(); u != nil {
			s.emit(map[string]interface{}{"model": s.model, "choices": []interface{}{}, "usage": u})
		}
		s.pending = append(s.pending, []byte("data: [DONE]\n\n")...)
		s.done = true
	}
	return nil
}
