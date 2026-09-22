package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/i18n"
)

// TestMessagesAreComplete is the cheap half of the completeness guarantee: the
// type system makes every message carry every language, and this pins that the
// fields are filled in.
func TestMessagesAreComplete(t *testing.T) {
	if len(Messages) == 0 {
		t.Fatal("the catalog is empty")
	}
	if incomplete := Messages.Audit(); len(incomplete) != 0 {
		t.Fatalf("incomplete messages: %v", incomplete)
	}
}

// TestMessagesMergeWithAnotherLayer keeps the merge honest: this catalog claims
// no id a front-end may want for itself.
func TestMessagesMergeWithAnotherLayer(t *testing.T) {
	shell := i18n.Catalog{"cli.example": {EN: "example", ZH: "示例"}}
	if _, err := i18n.Merge(Messages, shell); err != nil {
		t.Fatalf("merging the layers: %v", err)
	}
}

// TestNoOrphanMessages keeps the catalog honest in the other direction: a
// message no code renders is dead weight a translator would still maintain, so
// it is removed rather than kept "for later".
func TestNoOrphanMessages(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok {
				used[ident.Name] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	for name := range messageConstants(t) {
		if !used[name] {
			t.Errorf("service.%s is never rendered", name)
		}
	}
}

// messageConstants reads this catalog's source for the constant names, so the
// orphan check needs no second registry to drift out of date.
func messageConstants(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "messages.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing the catalog: %v", err)
	}
	found := map[string]string{}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			found[value.Names[0].Name] = strings.Trim(literal.Value, `"`)
		}
	}
	return found
}
