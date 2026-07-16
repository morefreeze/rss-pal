package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestProcessFeedWiresWeiboContentPolicy(t *testing.T) {
	body := parseFunctionBody(t, "main.go", "processFeed")

	if got := selectorCallCount(body, "rss", "BuildItemContent"); got < 2 {
		t.Fatalf("processFeed BuildItemContent calls = %d, want at least 2 (existing and new items)", got)
	}
	if got := selectorCallCount(body, "", "UpdateEnrichedContentIfChanged"); got < 1 {
		t.Fatal("processFeed does not refresh enriched content for existing items")
	}
	if got := selectorCallCount(body, "rss", "ShouldDeepFetchArticle"); got < 1 {
		t.Fatal("processFeed does not use the centralized deep-fetch policy")
	}
}

func TestRefetchShortContentWiresDeepFetchPolicy(t *testing.T) {
	body := parseFunctionBody(t, "main.go", "refetchShortContent")

	if got := selectorCallCount(body, "rss", "ShouldDeepFetchArticle"); got < 1 {
		t.Fatal("refetchShortContent does not use the centralized deep-fetch policy")
	}
}

func parseFunctionBody(t *testing.T, filename, functionName string) *ast.BlockStmt {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == functionName {
			return function.Body
		}
	}
	t.Fatalf("function %s not found in %s", functionName, filename)
	return nil
}

func selectorCallCount(node ast.Node, receiver, method string) int {
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
