package moonraker_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

const driverImport = `"github.com/mjonuschat/prusa-driver-moonraker/driver"`

// The composed build imports the module path, so the root must stay a package,
// but the logic belongs in driver/. This pins doc.go to one blank import.
func TestDocGoOnlyBlankImportsDriver(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../doc.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse doc.go: %v", err)
	}
	if len(file.Imports) != 1 {
		t.Fatalf("doc.go has %d imports, want exactly one", len(file.Imports))
	}
	spec := file.Imports[0]
	if spec.Path.Value != driverImport || spec.Name == nil || spec.Name.Name != "_" {
		t.Fatalf("doc.go import is %v, want a blank import of %s", spec.Path.Value, driverImport)
	}

	full, err := parser.ParseFile(token.NewFileSet(), "../doc.go", nil, 0)
	if err != nil {
		t.Fatalf("parse doc.go: %v", err)
	}
	for _, decl := range full.Decls {
		if gen, ok := decl.(*ast.GenDecl); !ok || gen.Tok != token.IMPORT {
			t.Fatalf("doc.go declares %T, want only the import", decl)
		}
	}
}
