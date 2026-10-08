package tool_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRawErrorIsEverSaid : What a tool hands back is read out loud,
// so none of it may be a Go error.
//
// There were 56 of them. A wrapped repository error reached the model
// as the whole of what to say, which means a database driver's
// "dial tcp 127.0.0.1:3306: i/o timeout" was a sentence the assistant
// would have spoken in its own voice.
//
// Failed takes what to say. Broke takes what to say and keeps the
// fault behind it, for the log and for somebody who asks what exactly
// went wrong. Neither takes err.Error(), and this is what says so.
func TestNoRawErrorIsEverSaid(t *testing.T) {
	root := "."
	var bad []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !calls(call.Fun, "Failed", "Broke", "Partial", "OK") {
				return true
			}
			// Broke's second argument is the error, and keeping it is
			// the whole point. Only what is said is checked.
			args := call.Args
			if calls(call.Fun, "Broke") && len(args) > 1 {
				args = args[:1]
			}
			for _, a := range args {
				if saysError(a) {
					bad = append(bad, fset.Position(call.Pos()).String())
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tools: %v", err)
	}

	for _, where := range bad {
		t.Errorf("%s hands a Go error to the person: say what went wrong and pass the error to tool.Broke", where)
	}
}

// calls : Whether this is a call to one of the given functions, with
// or without a package qualifier.
func calls(fun ast.Expr, want ...string) bool {
	var got string
	switch f := fun.(type) {
	case *ast.Ident:
		got = f.Name
	case *ast.SelectorExpr:
		got = f.Sel.Name
	default:
		return false
	}
	for _, w := range want {
		if got == w {
			return true
		}
	}
	return false
}

// saysError : Whether this expression puts an error's own words into
// what gets said, however deeply it is buried in a concatenation.
func saysError(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// err.Error(), anywhere inside.
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && len(call.Args) == 0 {
			found = true
		}
		return true
	})
	return found
}
