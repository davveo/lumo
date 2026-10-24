package constants_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Prevent a future production branch from silently reintroducing an inline
// persisted status while another branch uses the central enum. Fixtures retain
// literal protocol values so changing a declaration cannot update both sides
// of a compatibility assertion at once.
func TestProductionStatusesUseCentralDefinitions(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	directory := filepath.Dir(file)
	root := filepath.Join(directory, "..", "..")
	uppercase := regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)
	statuses := map[string]bool{}
	definitions, _ := filepath.Glob(filepath.Join(directory, "*.go"))
	for _, path := range definitions {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				value, _ := strconv.Unquote(literal.Value)
				if uppercase.MatchString(value) {
					statuses[value] = true
				}
			}
			return true
		})
	}
	for _, folder := range []string{"services", "internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, folder), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path == directory {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".pb.go") {
				return nil
			}
			positions := token.NewFileSet()
			parsed, err := parser.ParseFile(positions, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					value, _ := strconv.Unquote(literal.Value)
					if statuses[value] {
						t.Errorf("%s: inline business status %q; use its domain constant", positions.Position(literal.Pos()), value)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
