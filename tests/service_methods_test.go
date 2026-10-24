package tests

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/platform"
)

type serviceMethodContract struct {
	Services []struct {
		Service  string `json:"service"`
		Name     string `json:"name"`
		Existing bool   `json:"existing_contract"`
		Methods  []struct {
			Action string `json:"action"`
			Method string `json:"method"`
		} `json:"methods"`
	} `json:"services"`
}

func methodProjectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..")
}

func TestAllServicesExposeTheirCompleteNamedRPCContract(t *testing.T) {
	root := methodProjectRoot()
	data, err := os.ReadFile(filepath.Join(root, "api/mall/v1/service_methods.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract serviceMethodContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Services) != len(bootstrap.Names) {
		t.Fatal("method inventory omits a deployed service")
	}
	actions := map[string]string{}
	definitions, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/constants/actions.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(definitions, func(node ast.Node) bool {
		if value, ok := node.(*ast.ValueSpec); ok && len(value.Names) == 1 && len(value.Values) == 1 {
			if literal, ok := value.Values[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				actions[value.Names[0].Name], _ = strconv.Unquote(literal.Value)
			}
		}
		return true
	})
	seen := map[string]bool{}
	for _, spec := range contract.Services {
		t.Run(spec.Service, func(t *testing.T) {
			if seen[spec.Service] {
				t.Fatal("duplicate service contract")
			}
			seen[spec.Service] = true
			module, err := bootstrap.New(spec.Service, platform.Dependencies{})
			if err != nil {
				t.Fatal(err)
			}
			if !spec.Existing && len(module.Operations) != len(spec.Methods) {
				t.Fatal("RPC inventory and business method registrations differ")
			}
			server, err := platform.NewRPCServer("contract-test-token", module)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			info, ok := server.GetServiceInfo()["mall.v1."+spec.Name+"Service"]
			if !ok || len(info.Methods) != len(spec.Methods) {
				t.Fatalf("missing explicit gRPC service/methods: %+v", info)
			}
			if spec.Existing {
				return
			} // Order already has its own typed request/response contract.
			wireMethods := map[string]bool{}
			for _, method := range info.Methods {
				wireMethods[method.Name] = true
			}
			bindings := map[string]string{}
			handler, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "services", spec.Service, "internal/transport/handler.go"), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(handler, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				mapping, ok := literal.Type.(*ast.MapType)
				if !ok {
					return true
				}
				valueType, ok := mapping.Value.(*ast.SelectorExpr)
				if !ok || valueType.Sel.Name != "Operation" {
					return true
				}
				for _, element := range literal.Elts {
					entry, ok := element.(*ast.KeyValueExpr)
					if !ok {
						t.Fatal("operation registration must be explicit")
					}
					key, keyOK := entry.Key.(*ast.SelectorExpr)
					value, valueOK := entry.Value.(*ast.SelectorExpr)
					if !keyOK || !valueOK {
						t.Fatal("operation must bind directly to its business method")
					}
					bindings[key.Sel.Name] = value.Sel.Name
				}
				return true
			})
			for _, method := range spec.Methods {
				action, ok := actions[method.Action]
				if !ok || module.Operations[action] == nil || !wireMethods[method.Method] || bindings[method.Action] != method.Method {
					t.Fatalf("%s is not directly bound to %s in both usecase and RPC", method.Action, method.Method)
				}
			}
		})
	}
	for _, name := range bootstrap.Names {
		if !seen[name] {
			t.Errorf("missing contract for %s", name)
		}
	}
}

func TestBusinessLayersCannotReintroduceActionDispatch(t *testing.T) {
	root := methodProjectRoot()
	err := filepath.WalkDir(filepath.Join(root, "services"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.Contains(path, "/internal/usecase/") && !strings.Contains(path, "/internal/repository/") {
			return nil
		}
		positions := token.NewFileSet()
		parsed, err := parser.ParseFile(positions, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.SelectorExpr:
				if qualifier, ok := value.X.(*ast.Ident); ok && qualifier.Name == "platform" && value.Sel.Name == "Command" {
					t.Errorf("%s: business layer receives legacy action command", positions.Position(value.Pos()))
				}
				if value.Sel.Name == "Action" {
					t.Errorf("%s: business layer selects an action", positions.Position(value.Pos()))
				}
			case *ast.FuncDecl:
				if value.Recv != nil && (value.Name.Name == "Execute" || value.Name.Name == "Handle") {
					t.Errorf("%s: generic business dispatcher is forbidden", positions.Position(value.Pos()))
				}
			case *ast.CallExpr:
				if method, ok := value.Fun.(*ast.SelectorExpr); ok && method.Sel.Name == "Call" {
					t.Errorf("%s: use an explicit domain client method", positions.Position(value.Pos()))
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
