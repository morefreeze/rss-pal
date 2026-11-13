package main

import "testing"

func TestProcessQueuedChildrenRefreshesFetchedPageTitle(t *testing.T) {
	body := parseFunctionBody(t, "linkset.go", "fetchLinkChild")

	if got := selectorCallCount(body, "", "FetchContentWithMetadata"); got < 1 {
		t.Fatalf("fetchLinkChild FetchContentWithMetadata calls = %d, want at least 1", got)
	}
	if got := selectorCallCount(body, "", "CompleteLinkFetch"); got < 1 {
		t.Fatalf("fetchLinkChild CompleteLinkFetch calls = %d, want at least 1", got)
	}
}
