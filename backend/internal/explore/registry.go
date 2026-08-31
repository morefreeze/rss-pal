package explore

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

const StructuredProviderPriority = 300

// RegistryProvider is persisted provider state required for conditional sync.
type RegistryProvider struct {
	ID                        int
	Key, Kind, Endpoint       string
	Topic                     string
	SyncInterval              time.Duration
	Enabled                   bool
	ETag, LastModified        string
	LastSyncAt, LastSuccessAt *time.Time
	ConsecutiveFailures       int
}

// RegistryStore is intentionally narrow so provider sync can be tested without
// a database. Concrete SQL persistence lives in internal/repository.
type RegistryStore interface {
	LoadDueProviders(now time.Time) ([]RegistryProvider, error)
	UpsertCandidate(providerID int, candidate Candidate, observedAt time.Time) (sourceID int, err error)
	RecordSuccess(providerID int, syncedAt time.Time, etag, lastModified string) error
	RecordFailure(providerID int, syncedAt time.Time, cause error) error
}

type RegistryQueue interface {
	Enqueue(sourceID int, taskType string, priority int) error
}

// Registry syncs each due registry independently. A provider's error is kept
// in its result and database state without preventing later providers running.
type Registry struct {
	Store    RegistryStore
	Queue    RegistryQueue
	Client   ProviderClient
	Adapters map[string]ProviderAdapter
}

type ProviderSyncResult struct {
	ProviderID  int
	ProviderKey string
	NotModified bool
	Candidates  int
	Err         error
}

func DefaultProviderAdapters() map[string]ProviderAdapter {
	adapters := []ProviderAdapter{OPMLRegistryAdapter{}, DirectoryAdapter{}, RedditLinkStreamAdapter{}, GitHubAwesomeAdapter{}}
	result := make(map[string]ProviderAdapter, len(adapters))
	for _, adapter := range adapters {
		result[adapter.Kind()] = adapter
	}
	return result
}

func (r Registry) SyncDue(ctx context.Context, now time.Time) ([]ProviderSyncResult, error) {
	if r.Store == nil {
		return nil, fmt.Errorf("registry store is required")
	}
	providers, err := r.Store.LoadDueProviders(now)
	if err != nil {
		return nil, err
	}
	results := make([]ProviderSyncResult, 0, len(providers))
	for _, provider := range providers {
		results = append(results, r.syncOne(ctx, now, provider))
	}
	return results, nil
}

func (r Registry) syncOne(ctx context.Context, now time.Time, provider RegistryProvider) ProviderSyncResult {
	result := ProviderSyncResult{ProviderID: provider.ID, ProviderKey: provider.Key}
	adapter, exists := r.Adapters[provider.Kind]
	if !exists {
		result.Err = fmt.Errorf("unsupported provider kind %q", provider.Kind)
		r.recordFailure(provider.ID, now, result.Err)
		return result
	}
	fetched, err := r.Client.Fetch(ctx, provider.Endpoint, provider.ETag, provider.LastModified)
	if err != nil {
		result.Err = err
		r.recordFailure(provider.ID, now, err)
		return result
	}
	if fetched.NotModified {
		result.NotModified = true
		if err := r.Store.RecordSuccess(provider.ID, now, firstNonEmpty(fetched.ETag, provider.ETag), firstNonEmpty(fetched.LastModified, provider.LastModified)); err != nil {
			result.Err = err
		}
		return result
	}
	candidates, err := adapter.Parse(Provider{ID: provider.ID, Key: provider.Key, Kind: provider.Kind, Endpoint: provider.Endpoint, Topic: provider.Topic}, fetched.Body)
	if err != nil {
		result.Err = err
		r.recordFailure(provider.ID, now, err)
		return result
	}
	for _, candidate := range NormalizeCandidates(candidates) {
		sourceID, err := r.Store.UpsertCandidate(provider.ID, candidate, now)
		if err == nil && r.Queue != nil {
			err = r.Queue.Enqueue(sourceID, model.ExploreFetchTaskValidateSource, StructuredProviderPriority)
		}
		if err != nil {
			result.Err = err
			r.recordFailure(provider.ID, now, err)
			return result
		}
		result.Candidates++
	}
	if err := r.Store.RecordSuccess(provider.ID, now, fetched.ETag, fetched.LastModified); err != nil {
		result.Err = err
	}
	return result
}

func (r Registry) recordFailure(providerID int, now time.Time, cause error) {
	if r.Store != nil {
		_ = r.Store.RecordFailure(providerID, now, cause)
	}
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
