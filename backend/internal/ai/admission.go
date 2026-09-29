package ai

import (
	"context"
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/aiusage"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"io"
	"net/http"
	"strings"
	"sync"
)

type Admission func(context.Context) (func(), error)

var admissionMu sync.RWMutex
var defaultAdmission Admission
var defaultUsageRecorder func(aiusage.Record)

func ConfigureUsageRecorder(f func(aiusage.Record)) {
	admissionMu.Lock()
	defer admissionMu.Unlock()
	defaultUsageRecorder = f
}
func currentUsageRecorder() func(aiusage.Record) {
	admissionMu.RLock()
	defer admissionMu.RUnlock()
	return defaultUsageRecorder
}

// ConfigureAdmission runs at service startup, before constructing summarizers.
// Constructors copy the guard, including user-configured AI clients.
func ConfigureAdmission(f Admission) {
	admissionMu.Lock()
	defer admissionMu.Unlock()
	defaultAdmission = f
}
func currentAdmission() Admission {
	admissionMu.RLock()
	defer admissionMu.RUnlock()
	return defaultAdmission
}
func (s *Summarizer) SetAdmission(f Admission) { s.admission = f }

type admittedBody struct {
	io.ReadCloser
	release func()
}

func (b *admittedBody) Close() error { defer b.release(); return b.ReadCloser.Close() }
func (s *Summarizer) doAdmitted(req *http.Request) (*http.Response, error) {
	release := func() {}
	if s.admission != nil {
		var err error
		release, err = s.admission(req.Context())
		if err != nil {
			return nil, err
		}
	}
	r := aiusage.Record{Provider: strings.ToLower(req.URL.Hostname()), Model: s.model, Owner: taskbudget.Owner(req.Context())}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			var m struct {
				Model string `json:"model"`
			}
			if json.NewDecoder(body).Decode(&m) == nil && m.Model != "" {
				r.Model = m.Model
			}
			body.Close()
		}
	}
	var response *http.Response
	var err error
	if s.providerProtocol == "anthropic" {
		response, err = s.doAnthropic(req)
	} else {
		response, err = s.doCompatible(req)
	}
	if err != nil {
		release()
		if s.usageRecorder != nil {
			s.usageRecorder(r)
		}
		return nil, err
	}
	if s.usageRecorder != nil {
		response.Body = aiusage.Capture(response.Body, strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") || req.Header.Get("Accept") == "text/event-stream", r, s.usageRecorder)
	}
	response.Body = &admittedBody{ReadCloser: response.Body, release: release}
	return response, nil
}
