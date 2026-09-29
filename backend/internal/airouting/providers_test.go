package airouting

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestModelsUseOfficialEndpointsAndFilter(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.z.ai/api/coding/paas/v4/models" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("incorrect endpoint or auth")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"glm-5.3"},{"id":"embedding-3"},{"id":"glm-5.3"},{"id":"glm-image"},{"id":"glm-5.3-flash"}]}`))}, nil
	})}
	result, err := FetchModels(context.Background(), client, "zai", "coding", "test-secret")
	if err != nil || len(result.Models) != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err = FetchModels(context.Background(), client, "zai", "https://evil.invalid", "secret"); err == nil {
		t.Fatal("accepted arbitrary endpoint")
	}
}
func TestClaudePagination(t *testing.T) {
	calls := 0
	c := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("x-api-key") != "key" || r.Header.Get("anthropic-version") == "" {
			t.Fatal("auth")
		}
		body := `{"data":[{"id":"claude-a","display_name":"A"}],"has_more":true,"last_id":"claude-a"}`
		if calls == 2 {
			if r.URL.Query().Get("after_id") != "claude-a" {
				t.Fatal("cursor")
			}
			body = `{"data":[{"id":"claude-b"}],"has_more":false}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	r, e := FetchModels(context.Background(), c, "anthropic", "default", "key")
	if e != nil || len(r.Models) != 2 || calls != 2 {
		t.Fatalf("%+v %v %d", r, e, calls)
	}
}
func TestModelsDoNotEchoUpstreamSecrets(t *testing.T) {
	c := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret-key"))}, nil
	})}
	_, err := FetchModels(context.Background(), c, "openai", "default", "secret-key")
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatal("unsafe error")
	}
}

func TestBailianAndXAIListShapes(t *testing.T) {
	for _, provider := range []string{"bailian", "xai"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			c := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				body := `{"models":[{"id":"grok-4","name":"Grok 4"}]}`
				if provider == "bailian" {
					if r.URL.Query().Get("capabilities") != "TG" {
						t.Fatal("not filtered text")
					}
					if calls == 1 {
						body = `{"success":true,"output":{"total":101,"models":[{"model":"qwen-plus","name":"Qwen Plus"}]}}`
					} else {
						if r.URL.Query().Get("page_no") != "2" {
							t.Fatal("wrong page")
						}
						body = `{"success":true,"output":{"total":101,"models":[{"model":"qwen-max","name":"Qwen Max"}]}}`
					}
				} else if r.URL.Path != "/v1/language-models" {
					t.Fatal("wrong grok endpoint")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			endpoint := "default"
			want := 1
			if provider == "bailian" {
				endpoint = "china"
				want = 2
			}
			result, err := FetchModels(context.Background(), c, provider, endpoint, "key")
			if err != nil || len(result.Models) != want {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
