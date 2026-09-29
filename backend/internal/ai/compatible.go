package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// OpenAI reasoning models renamed max_tokens. Keep legacy/custom compatible
// providers on their existing schema; only adapt the official OpenAI endpoint.
func (s *Summarizer) doCompatible(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() == "api.openai.com" && (strings.HasPrefix(s.model, "gpt-5") || strings.HasPrefix(s.model, "o1") || strings.HasPrefix(s.model, "o3") || strings.HasPrefix(s.model, "o4")) {
		var payload map[string]any
		err := json.NewDecoder(req.Body).Decode(&payload)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		payload["max_completion_tokens"] = payload["max_tokens"]
		delete(payload, "max_tokens")
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	client := *s.httpClient
	if s.articleRouted {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client.Do(req)
}
