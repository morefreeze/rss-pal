package main

import (
	"go/ast"
	"testing"
)

func TestAsyncSummarizeWritesSummaryWithContentCAS(t *testing.T) {
	body := parseFunctionBody(t, "main.go", "asyncSummarize")
	assertGeneratedSummaryUsesCAS(t, body, func(argument ast.Expr) bool {
		identifier, ok := argument.(*ast.Ident)
		return ok && identifier.Name == "content"
	})
}

func TestBackfillSummariesWritesSummaryWithContentCAS(t *testing.T) {
	body := parseFunctionBody(t, "main.go", "backfillSummaries")
	assertGeneratedSummaryUsesCAS(t, body, func(argument ast.Expr) bool {
		selector, ok := argument.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Content" {
			return false
		}
		identifier, ok := selector.X.(*ast.Ident)
		return ok && identifier.Name == "article"
	})
}

func assertGeneratedSummaryUsesCAS(t *testing.T, body *ast.BlockStmt, matchesExpectedContent func(ast.Expr) bool) {
	t.Helper()

	if got := selectorCallCount(body, "", "UpdateSummary"); got != 0 {
		t.Fatalf("generated summary function has %d unconditional UpdateSummary calls, want 0", got)
	}
	calls := selectorCalls(body, "UpdateSummaryIfContentUnchanged")
	if len(calls) != 1 {
		t.Fatalf("UpdateSummaryIfContentUnchanged calls = %d, want 1", len(calls))
	}
	if len(calls[0].Args) < 2 || !matchesExpectedContent(calls[0].Args[1]) {
		t.Fatal("UpdateSummaryIfContentUnchanged does not receive the generated content snapshot")
	}
}

func selectorCalls(node ast.Node, method string) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == method {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}
