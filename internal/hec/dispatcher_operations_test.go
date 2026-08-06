package hec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	hecschemas "github.com/StealthEyeLLC/hec/schemas"
)

func TestDispatcherOperationsMatchEmbeddedSchema(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Join(filepath.Dir(currentFile), "dispatcher.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var dispatcherOperations []string
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Dispatch" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				operation, err := strconv.Unquote(literal.Value)
				if err == nil {
					dispatcherOperations = append(dispatcherOperations, operation)
				}
			}
			return true
		})
	}
	sort.Strings(dispatcherOperations)
	if len(dispatcherOperations) != 38 {
		t.Fatalf("dispatcher operations = %d, want 38: %#v", len(dispatcherOperations), dispatcherOperations)
	}
	for index := 1; index < len(dispatcherOperations); index++ {
		if dispatcherOperations[index] == dispatcherOperations[index-1] {
			t.Fatalf("duplicate dispatcher operation %q", dispatcherOperations[index])
		}
	}
	cards, err := extractOperationCapabilities(hecschemas.CallHECInput)
	if err != nil {
		t.Fatal(err)
	}
	schemaOperations := make([]string, 0, len(cards))
	for _, card := range cards {
		schemaOperations = append(schemaOperations, card.Operation)
	}
	sort.Strings(schemaOperations)
	if !reflect.DeepEqual(dispatcherOperations, schemaOperations) {
		t.Fatalf("dispatcher operations = %#v, schema operations = %#v", dispatcherOperations, schemaOperations)
	}
	for _, operation := range dispatcherOperations {
		if operation == "browser.open" {
			t.Fatalf("browser operation advertised: %s", operation)
		}
		for _, forbidden := range []string{"workspace.", "repository.", "delivery.", "worktree."} {
			if strings.HasPrefix(operation, forbidden) {
				t.Fatalf("Slice 8 controller operation advertised: %s", operation)
			}
		}
	}
}
