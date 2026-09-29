package airouting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type Endpoint struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BaseURL   string `json:"-"`
	ModelsURL string `json:"-"`
}
type Provider struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Endpoints []Endpoint `json:"endpoints"`
	Protocol  string     `json:"-"`
}

// Only fixed official origins are permitted. No admin-supplied URL or redirect
// can send a provider credential to another host.
var Providers = []Provider{
	{"zai", "智谱 / Z.AI", []Endpoint{
		{"coding", "Z.AI Coding Plan", "https://api.z.ai/api/coding/paas/v4", "https://api.z.ai/api/coding/paas/v4/models"},
		{"standard", "Z.AI 按量 API", "https://api.z.ai/api/paas/v4", "https://api.z.ai/api/paas/v4/models"},
		{"china", "智谱国内按量 API", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/models"},
	}, "openai"},
	{"openai", "OpenAI", []Endpoint{{"default", "OpenAI API", "https://api.openai.com/v1", "https://api.openai.com/v1/models"}}, "openai"},
	{"deepseek", "DeepSeek", []Endpoint{{"default", "DeepSeek API", "https://api.deepseek.com", "https://api.deepseek.com/models"}}, "openai"},
	{"bailian", "阿里云百炼 / 通义", []Endpoint{
		{"china", "中国内地（北京）", "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/api/v1/models"},
		{"international", "国际（新加坡）", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "https://dashscope-intl.aliyuncs.com/api/v1/models"},
	}, "openai"},
	{"anthropic", "Anthropic / Claude", []Endpoint{{"default", "Claude API", "https://api.anthropic.com/v1", "https://api.anthropic.com/v1/models"}}, "anthropic"},
	{"xai", "xAI / Grok", []Endpoint{{"default", "xAI API", "https://api.x.ai/v1", "https://api.x.ai/v1/language-models"}}, "openai"},
}

func Lookup(provider, endpoint string) (Provider, Endpoint, error) {
	for _, p := range Providers {
		if p.ID == provider {
			for _, e := range p.Endpoints {
				if e.ID == endpoint {
					return p, e, nil
				}
			}
		}
	}
	return Provider{}, Endpoint{}, errors.New("请选择有效的公司和接入方式")
}

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ModelList struct {
	Models []Model `json:"models"`
	Source string  `json:"source"`
}

func ValidModel(provider, id string) bool {
	if id == "" || len(id) > 200 || strings.ContainsAny(id, "\r\n\t ") {
		return false
	}
	lower := strings.ToLower(id)
	for _, bad := range []string{"embedding", "moderation", "whisper", "tts", "dall-e", "image", "imagine", "video", "audio", "realtime", "transcri", "ocr", "asr", "sora"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	switch provider {
	case "openai":
		if strings.Contains(lower, "codex") || strings.Contains(lower, "-pro") || strings.Contains(lower, "deep-research") || strings.Contains(lower, "-search") {
			return false
		}
		return strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "chatgpt-") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4")
	case "zai":
		return strings.HasPrefix(lower, "glm-")
	case "anthropic":
		return strings.HasPrefix(lower, "claude-")
	case "deepseek":
		return strings.HasPrefix(lower, "deepseek-")
	case "xai":
		return strings.HasPrefix(lower, "grok-")
	case "bailian":
		return strings.HasPrefix(lower, "qwen") || strings.HasPrefix(lower, "qwq") || strings.HasPrefix(lower, "deepseek-") || strings.HasPrefix(lower, "glm-") || strings.HasPrefix(lower, "kimi-")
	}
	return false
}
func FetchModels(ctx context.Context, client *http.Client, provider, endpoint, key string) (ModelList, error) {
	p, e, err := Lookup(provider, endpoint)
	if err != nil {
		return ModelList{}, err
	}
	if key == "" {
		return ModelList{}, errors.New("请先填写该公司的 API Key")
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	result := ModelList{Models: []Model{}, Source: "公司 API 实时列表"}
	seen := map[string]bool{}
	cursor := ""
	for page := 1; page <= 50; page++ {
		u, _ := url.Parse(e.ModelsURL)
		q := u.Query()
		if p.ID == "anthropic" {
			q.Set("limit", "1000")
			if cursor != "" {
				q.Set("after_id", cursor)
			}
		}
		if p.ID == "bailian" {
			q.Set("page_no", fmt.Sprint(page))
			q.Set("page_size", "100")
			q.Set("capabilities", "TG")
		}
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return ModelList{}, err
		}
		if p.Protocol == "anthropic" {
			req.Header.Set("x-api-key", key)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := safeClient.Do(req)
		if err != nil {
			return ModelList{}, errors.New("连接模型列表接口失败，请稍后重试")
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return ModelList{}, fmt.Errorf("模型列表接口返回 HTTP %d，请检查 API Key、接入方式和账号权限", resp.StatusCode)
		}
		if readErr != nil || len(data) > 4<<20 {
			return ModelList{}, errors.New("模型列表响应读取失败")
		}
		var body struct {
			Data []struct {
				ID   string `json:"id"`
				Name string `json:"display_name"`
			} `json:"data"`
			Models []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"models"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
			Success *bool  `json:"success"`
			Output  struct {
				Total  int `json:"total"`
				Models []struct {
					ID   string `json:"model"`
					Name string `json:"name"`
				} `json:"models"`
			} `json:"output"`
		}
		if json.Unmarshal(data, &body) != nil || (body.Success != nil && !*body.Success) {
			return ModelList{}, errors.New("模型列表接口响应无效")
		}
		add := func(id, name string) {
			if !seen[id] && ValidModel(provider, id) {
				if name == "" {
					name = id
				}
				result.Models = append(result.Models, Model{id, name})
				seen[id] = true
			}
		}
		for _, m := range body.Data {
			add(m.ID, m.Name)
		}
		for _, m := range body.Models {
			add(m.ID, m.Name)
		}
		for _, m := range body.Output.Models {
			add(m.ID, m.Name)
		}
		more := false
		if p.ID == "anthropic" && body.HasMore {
			if body.LastID == "" || body.LastID == cursor {
				return ModelList{}, errors.New("模型列表分页响应无效")
			}
			cursor = body.LastID
			more = true
		}
		if p.ID == "bailian" && page*100 < body.Output.Total {
			if len(body.Output.Models) == 0 {
				return ModelList{}, errors.New("模型列表分页响应无效")
			}
			more = true
		}
		if !more {
			sort.Slice(result.Models, func(i, j int) bool { return result.Models[i].ID < result.Models[j].ID })
			if len(result.Models) == 0 {
				return ModelList{}, errors.New("该账号未返回可用于文章摘要的模型")
			}
			return result, nil
		}
	}
	return ModelList{}, errors.New("模型列表超过分页上限，请稍后重试")
}
