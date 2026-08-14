package main

import "testing"

func TestProcessQueuedChildrenRefreshesFetchedPageTitle(t *testing.T) {
	body := parseFunctionBody(t, "linkset.go", "processQueuedChildren")

	if got := selectorCallCount(body, "", "FetchContentWithMetadata"); got < 1 {
		t.Fatalf("processQueuedChildren FetchContentWithMetadata calls = %d, want at least 1", got)
	}
	if got := selectorCallCount(body, "", "UpdateTitle"); got < 1 {
		t.Fatalf("processQueuedChildren UpdateTitle calls = %d, want at least 1", got)
	}
}
