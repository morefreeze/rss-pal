package main

import (
	"context"
	"go/ast"
	"sync"
	"testing"
	"time"
)

type heartbeatWriterRecorder struct {
	mu         sync.Mutex
	components []string
	beats      chan struct{}
}

func (r *heartbeatWriterRecorder) Beat(component string) error {
	r.mu.Lock()
	r.components = append(r.components, component)
	r.mu.Unlock()
	r.beats <- struct{}{}
	return nil
}

func (r *heartbeatWriterRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.components)
}

func TestWorkerHeartbeatStartsImmediatelyTicksAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := &heartbeatWriterRecorder{beats: make(chan struct{}, 3)}

	startWorkerHeartbeatWithInterval(ctx, recorder, 5*time.Millisecond)
	waitForWorkerHeartbeat(t, recorder.beats, "startup")
	waitForWorkerHeartbeat(t, recorder.beats, "ticker")

	cancel()
	time.Sleep(20 * time.Millisecond)
	if got := recorder.count(); got != 2 {
		t.Fatalf("beats after cancellation = %d, want 2", got)
	}
}

func TestStartWorkerHeartbeatUsesOneMinuteInterval(t *testing.T) {
	body := parseFunctionBody(t, "main.go", "startWorkerHeartbeat")

	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok || identifier.Name != "startWorkerHeartbeatWithInterval" || len(call.Args) != 3 {
			return true
		}
		selector, ok := call.Args[2].(*ast.SelectorExpr)
		if ok {
			if packageName, ok := selector.X.(*ast.Ident); ok && packageName.Name == "time" && selector.Sel.Name == "Minute" {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Fatal("startWorkerHeartbeat does not use a one-minute ticker interval")
	}
}

func TestMainWiresWorkerHeartbeat(t *testing.T) {
	body := parseFunctionBody(t, "main.go", "main")

	if got := selectorCallCount(body, "repository", "NewServiceHeartbeatRepository"); got != 1 {
		t.Fatalf("NewServiceHeartbeatRepository calls = %d, want 1", got)
	}
	if got := identCallCount(body, "startWorkerHeartbeat"); got != 1 {
		t.Fatalf("startWorkerHeartbeat calls = %d, want 1", got)
	}
	if got := selectorCallCount(body, "context", "WithCancel"); got < 1 {
		t.Fatal("main does not create a cancellable worker heartbeat context")
	}
}

func waitForWorkerHeartbeat(t *testing.T, beats <-chan struct{}, phase string) {
	t.Helper()
	select {
	case <-beats:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s worker heartbeat", phase)
	}
}

func identCallCount(node ast.Node, function string) int {
	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && identifier.Name == function {
			count++
		}
		return true
	})
	return count
}
