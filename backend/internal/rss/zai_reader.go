package rss

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	defaultZAIReaderMCPURL = "https://api.z.ai/api/mcp/web_reader/mcp"
	zaiReaderBodyLimit     = 8 << 20
)

var zaiReaderSlots = make(chan struct{}, 2)

type zaiReader struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type zaiReaderResult struct {
	Title   string          `json:"title"`
	Content string          `json:"content"`
	Error   json.RawMessage `json:"error"`
}

func newZAIReader(apiKey, endpoint string, client *http.Client) *zaiReader {
	if endpoint == "" {
		endpoint = defaultZAIReaderMCPURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &zaiReader{
		apiKey:   strings.TrimSpace(apiKey),
		endpoint: endpoint,
		client:   client,
	}
}

func (r *zaiReader) fetch(ctx context.Context, target string, noCache bool) (ContentResult, error) {
	if r == nil || r.apiKey == "" {
		return ContentResult{}, errors.New("z.ai reader is not configured")
	}

	select {
	case zaiReaderSlots <- struct{}{}:
		defer func() { <-zaiReaderSlots }()
	case <-ctx.Done():
		return ContentResult{}, fmt.Errorf("z.ai reader wait: %w", ctx.Err())
	}

	initialize := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "rss-pal",
				"version": "1",
			},
		},
	}
	initializeResponse, sessionID, err := r.call(ctx, "initialize", initialize, "")
	if err != nil {
		return ContentResult{}, err
	}
	if initializeResponse.Error != nil {
		return ContentResult{}, fmt.Errorf("z.ai reader initialize: json-rpc error %d", initializeResponse.Error.Code)
	}
	if sessionID == "" {
		return ContentResult{}, errors.New("z.ai reader initialize: missing session id")
	}

	initialized := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	}
	if err := r.notify(ctx, "initialized notification", initialized, sessionID); err != nil {
		return ContentResult{}, err
	}

	toolCall := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "webReader",
			"arguments": map[string]any{
				"url":                 target,
				"timeout":             20,
				"no_cache":            noCache,
				"return_format":       "markdown",
				"retain_images":       true,
				"no_gfm":              false,
				"keep_img_data_url":   false,
				"with_images_summary": false,
				"with_links_summary":  false,
			},
		},
	}
	toolResponse, _, err := r.call(ctx, "tool call", toolCall, sessionID)
	if err != nil {
		return ContentResult{}, err
	}
	if toolResponse.Error != nil {
		return ContentResult{}, fmt.Errorf("z.ai reader tool call: json-rpc error %d", toolResponse.Error.Code)
	}

	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(toolResponse.Result, &result); err != nil {
		return ContentResult{}, errors.New("z.ai reader tool call: malformed result")
	}
	if result.IsError {
		return ContentResult{}, errors.New("z.ai reader tool call: tool returned an error")
	}

	var nestedText string
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			nestedText = block.Text
			break
		}
	}
	if nestedText == "" {
		return ContentResult{}, errors.New("z.ai reader tool call: missing text result")
	}

	readerResult, err := decodeZAIReaderResult(nestedText)
	if err != nil {
		return ContentResult{}, err
	}
	if len(readerResult.Error) > 0 && string(readerResult.Error) != "null" {
		return ContentResult{}, errors.New("z.ai reader tool call: reader returned an error")
	}
	if strings.TrimSpace(readerResult.Content) == "" {
		return ContentResult{}, errors.New("z.ai reader tool call: empty content")
	}

	return ContentResult{Content: readerResult.Content, Title: readerResult.Title}, nil
}

func decodeZAIReaderResult(text string) (zaiReaderResult, error) {
	raw := []byte(text)
	for range 2 {
		var result zaiReaderResult
		if err := json.Unmarshal(raw, &result); err == nil {
			return result, nil
		}

		var wrapped string
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			break
		}
		raw = []byte(wrapped)
	}
	return zaiReaderResult{}, errors.New("z.ai reader tool call: malformed reader result")
}

func (r *zaiReader) call(ctx context.Context, stage string, payload any, sessionID string) (mcpResponse, string, error) {
	response, returnedSessionID, err := r.post(ctx, stage, payload, sessionID, true)
	if err != nil {
		return mcpResponse{}, "", err
	}
	var decoded mcpResponse
	if err := json.Unmarshal(response, &decoded); err != nil {
		return mcpResponse{}, "", fmt.Errorf("z.ai reader %s: malformed response", stage)
	}
	return decoded, returnedSessionID, nil
}

func (r *zaiReader) notify(ctx context.Context, stage string, payload any, sessionID string) error {
	_, _, err := r.post(ctx, stage, payload, sessionID, false)
	return err
}

func (r *zaiReader) post(ctx context.Context, stage string, payload any, sessionID string, wantResponse bool) ([]byte, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("z.ai reader %s: encode request", stage)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("z.ai reader %s: create request", stage)
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("z.ai reader %s: request failed: %w", stage, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("z.ai reader %s: http status %d", stage, resp.StatusCode)
	}
	returnedSessionID := resp.Header.Get("Mcp-Session-Id")
	if !wantResponse {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, zaiReaderBodyLimit))
		return nil, returnedSessionID, nil
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, zaiReaderBodyLimit))
	if err != nil {
		return nil, "", fmt.Errorf("z.ai reader %s: read response", stage)
	}
	decoded, err := decodeMCPBody(raw)
	if err != nil {
		return nil, "", fmt.Errorf("z.ai reader %s: malformed response", stage)
	}
	return decoded, returnedSessionID, nil
}

func decodeMCPBody(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("empty response")
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return trimmed, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	scanner.Buffer(make([]byte, 64*1024), zaiReaderBodyLimit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if json.Valid([]byte(data)) {
			return []byte(data), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("missing SSE data")
}
