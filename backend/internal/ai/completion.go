package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// completionError is terminal for the ordinary transport retry loop. Only a
// token-limit finish is eligible for one separate request with more headroom.
type completionError struct{ reason string }

func (e *completionError) Error() string {
	return fmt.Sprintf("AI 输出未完成（%s），请重试生成总结", e.reason)
}
func isCompletionError(err error) bool { var e *completionError; return errors.As(err, &e) }
func checkFinishReason(reason string) error {
	// Empty is supported for older OpenAI-compatible providers that omit it.
	if reason == "" || reason == "stop" {
		return nil
	}
	return &completionError{reason: reason}
}

// doCall retries a token-limited nonstream response once from scratch. Nothing
// from the incomplete attempt is returned or persisted. Each HTTP body closes
// before the next admission, so concurrency leases are never held over retries.
func (s *Summarizer) doCall(ctx context.Context, body []byte, maxTokens int) (string, error) {
	result, err := s.doCallOnce(ctx, body, maxTokens)
	var incomplete *completionError
	if !errors.As(err, &incomplete) || incomplete.reason != "length" || maxTokens >= 16000 {
		return result, err
	}
	var req chatRequest
	if decodeErr := json.Unmarshal(body, &req); decodeErr != nil {
		return "", decodeErr
	}
	req.MaxTokens = min(maxTokens*2, 16000)
	retryBody, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		return "", marshalErr
	}
	return s.doCallOnce(ctx, retryBody, req.MaxTokens)
}

// A terminal event is required even if the underlying HTTP connection closes
// cleanly. Already emitted deltas are previews; an error prevents persistence.
func readCompletionStream(body io.Reader, onDelta func(string)) (string, error) {
	var full strings.Builder
	reader := bufio.NewReader(body)
	finished := false
	var finishErr error
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return full.String(), err
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				finished = true
				break
			}
			var chunk streamChunk
			if json.Unmarshal([]byte(payload), &chunk) != nil {
				return full.String(), &completionError{reason: "invalid_stream"}
			}
			for _, ch := range chunk.Choices {
				if ch.Delta.Content != "" {
					full.WriteString(ch.Delta.Content)
					onDelta(ch.Delta.Content)
				}
				if ch.FinishReason != "" {
					finished = true
					if e := checkFinishReason(ch.FinishReason); e != nil {
						finishErr = e
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
	if finishErr != nil {
		return full.String(), finishErr
	}
	if !finished {
		return full.String(), &completionError{reason: "interrupted_stream"}
	}
	return full.String(), nil
}
