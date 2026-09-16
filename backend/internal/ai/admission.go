package ai

import (
	"context"
	"io"
	"net/http"
	"sync"
)

type Admission func(context.Context) (func(), error)

var admissionMu sync.RWMutex
var defaultAdmission Admission

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
	if s.admission == nil {
		return s.httpClient.Do(req)
	}
	release, err := s.admission(req.Context())
	if err != nil {
		return nil, err
	}
	response, err := s.httpClient.Do(req)
	if err != nil {
		release()
		return nil, err
	}
	response.Body = &admittedBody{ReadCloser: response.Body, release: release}
	return response, nil
}
