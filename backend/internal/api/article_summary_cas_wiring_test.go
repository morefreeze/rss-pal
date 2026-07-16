package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestGenerateSummaryWritesGeneratedResultsWithContentCAS(t *testing.T) {
	body := parseArticleFunctionBody(t, "GenerateSummary")

	assertArticleSummaryCASCalls(t, body, 2)
	if got := articleSelectorUseCount(body, "http", "StatusConflict"); got < 1 {
		t.Fatal("GenerateSummary does not return HTTP 409 for a stale generated summary")
	}
}

func TestStreamSummaryWritesGeneratedResultWithContentCASBeforeDone(t *testing.T) {
	body := parseArticleFunctionBody(t, "streamSummary")

	calls := assertArticleSummaryCASCalls(t, body, 1)
	donePosition := frameTypePosition(body, "done")
	if !donePosition.IsValid() {
		t.Fatal("streamSummary done frame not found")
	}
	if calls[0].Pos() >= donePosition {
		t.Fatal("streamSummary sends done before the content CAS write")
	}
}

func parseArticleFunctionBody(t *testing.T, functionName string) *ast.BlockStmt {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "article.go", nil, 0)
	if err != nil {
		t.Fatalf("parse article.go: %v", err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == functionName {
			return function.Body
		}
	}
	t.Fatalf("function %s not found in article.go", functionName)
	return nil
}

func assertArticleSummaryCASCalls(t *testing.T, body *ast.BlockStmt, want int) []*ast.CallExpr {
	t.Helper()

	if got := articleSelectorUseCount(body, "", "UpdateSummary"); got != 0 {
		t.Fatalf("generated summary function has %d unconditional UpdateSummary calls, want 0", got)
	}
	calls := articleSelectorCalls(body, "UpdateSummaryIfContentUnchanged")
	if len(calls) != want {
		t.Fatalf("UpdateSummaryIfContentUnchanged calls = %d, want %d", len(calls), want)
	}
	for _, call := range calls {
		if len(call.Args) < 2 || !isArticleContentSelector(call.Args[1]) {
			t.Fatal("UpdateSummaryIfContentUnchanged does not receive article.Content")
		}
	}
	return calls
}

func articleSelectorCalls(node ast.Node, method string) []*ast.CallExpr {
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

func articleSelectorUseCount(node ast.Node, receiver, selectorName string) int {
	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != selectorName {
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

func isArticleContentSelector(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Content" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == "article"
}

func frameTypePosition(node ast.Node, frameType string) token.Pos {
	position := token.NoPos
	ast.Inspect(node, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok || basicLiteralValue(pair.Key) != "type" || basicLiteralValue(pair.Value) != frameType {
				continue
			}
			position = literal.Pos()
			return false
		}
		return true
	})
	return position
}

func basicLiteralValue(expression ast.Expr) string {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING || len(literal.Value) < 2 {
		return ""
	}
	return literal.Value[1 : len(literal.Value)-1]
}
