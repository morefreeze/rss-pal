package explore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRegistrySyncDueContinuesAfterProviderFailureAndEnqueuesCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("registry")) }))
	defer server.Close()
	store := &registryStoreStub{providers: []RegistryProvider{
		{ID: 1, Key: "broken", Kind: "broken", Endpoint: server.URL},
		{ID: 2, Key: "good", Kind: "good", Endpoint: server.URL, Topic: "go"},
	}}
	queue := &registryQueueStub{}
	registry := Registry{Store: store, Queue: queue, Client: testProviderClient(server.Client()), Adapters: map[string]ProviderAdapter{
		"broken": adapterStub{kind: "broken", err: errors.New("bad document")},
		"good":   adapterStub{kind: "good", candidates: []Candidate{{ExternalKey: "feed", FeedURL: "https://example.com/feed", Title: "Example"}}},
	}}
	results, err := registry.SyncDue(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("SyncDue() error = %v", err)
	}
	if len(results) != 2 || results[0].Err == nil || results[1].Err != nil {
		t.Fatalf("results = %#v", results)
	}
	if len(store.failures) != 1 || len(store.successes) != 1 || len(store.upserts) != 1 {
		t.Fatalf("store = %#v", store)
	}
	if len(queue.items) != 1 || queue.items[0].priority != StructuredProviderPriority {
		t.Fatalf("queue = %#v", queue.items)
	}
}

func TestRegistrySyncDueDoesNotEnqueueOn304(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) }))
	defer server.Close()
	store := &registryStoreStub{providers: []RegistryProvider{{ID: 1, Key: "good", Kind: "good", Endpoint: server.URL}}}
	queue := &registryQueueStub{}
	registry := Registry{Store: store, Queue: queue, Client: testProviderClient(server.Client()), Adapters: map[string]ProviderAdapter{"good": adapterStub{kind: "good"}}}
	results, err := registry.SyncDue(context.Background(), time.Now())
	if err != nil || len(results) != 1 || !results[0].NotModified {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	if len(queue.items) != 0 || len(store.upserts) != 0 || len(store.successes) != 1 {
		t.Fatalf("store=%#v queue=%#v", store, queue)
	}
}

type registryStoreStub struct {
	providers           []RegistryProvider
	upserts             []Candidate
	successes, failures []int
}

func (s *registryStoreStub) LoadDueProviders(time.Time) ([]RegistryProvider, error) {
	return s.providers, nil
}
func (s *registryStoreStub) UpsertCandidate(_ int, candidate Candidate, _ time.Time) (int, error) {
	s.upserts = append(s.upserts, candidate)
	return 42, nil
}
func (s *registryStoreStub) RecordSuccess(id int, _ time.Time, _, _ string) error {
	s.successes = append(s.successes, id)
	return nil
}
func (s *registryStoreStub) RecordFailure(id int, _ time.Time, _ error) error {
	s.failures = append(s.failures, id)
	return nil
}

type registryQueueStub struct {
	items []struct {
		id       int
		task     string
		priority int
	}
}

func (q *registryQueueStub) Enqueue(id int, task string, priority int) error {
	q.items = append(q.items, struct {
		id       int
		task     string
		priority int
	}{id, task, priority})
	return nil
}

type adapterStub struct {
	kind       string
	candidates []Candidate
	err        error
}

func (a adapterStub) Kind() string                                { return a.kind }
func (a adapterStub) Parse(Provider, []byte) ([]Candidate, error) { return a.candidates, a.err }
