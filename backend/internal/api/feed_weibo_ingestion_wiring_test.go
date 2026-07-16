package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestFetchNowWiresWeiboContentPolicy(t *testing.T) {
	body := parseFeedFunctionBody(t, "FetchNow")

	if got := feedSelectorCallCount(body, "rss", "BuildItemContent"); got < 2 {
		t.Fatalf("FetchNow BuildItemContent calls = %d, want at least 2 (existing and new items)", got)
	}
	if got := feedSelectorCallCount(body, "rss", "ShouldDeepFetchArticle"); got < 1 {
		t.Fatal("FetchNow does not use the centralized deep-fetch policy")
	}
	if !bestEffortContainsSelectorCall(body, "UpdateEnrichedContentIfChanged") {
		t.Fatal("FetchNow enriched update is not protected by bestEffort")
	}
}

func parseFeedFunctionBody(t *testing.T, functionName string) *ast.BlockStmt {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "feed.go", nil, 0)
	if err != nil {
		t.Fatalf("parse feed.go: %v", err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == functionName {
			return function.Body
		}
	}
	t.Fatalf("function %s not found in feed.go", functionName)
	return nil
}

func feedSelectorCallCount(node ast.Node, receiver, method string) int {
	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != method {
			return true
		}
		if receiver != "" {
			identifier, ok := selector.X.(*ast.Ident)
			if !ok || identifier.Name != receiver {
				return true
			}
		}
		count++
		return true
	})
	return count
}

func bestEffortContainsSelectorCall(node ast.Node, method string) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok || identifier.Name != "bestEffort" {
			return true
		}
		for _, argument := range call.Args {
			callback, ok := argument.(*ast.FuncLit)
			if ok && feedSelectorCallCount(callback.Body, "", method) > 0 {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
