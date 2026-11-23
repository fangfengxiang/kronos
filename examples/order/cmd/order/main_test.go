package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestCoreAssemblyFitsThirtyLines(t *testing.T) {
	files := token.NewFileSet()
	source, err := parser.ParseFile(files, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range source.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "run" {
			continue
		}
		lines := files.Position(function.End()).Line - files.Position(function.Pos()).Line + 1
		if lines > 30 {
			t.Fatalf("Order assembly uses %d lines, want at most 30", lines)
		}
		return
	}
	t.Fatal("Order assembly function was not found")
}
