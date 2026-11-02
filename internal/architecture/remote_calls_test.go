package architecture

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This guard follows local receiver methods, function literals and operation
// arguments. Moving RPC into a helper does not remove a loop/transaction error.
type function struct {
	key, pkg   string
	body       *ast.BlockStmt
	recv       string
	types      map[string]string
	funcs      map[string]string
	params     []string
	operation  map[string]bool
	edges      map[string]bool
	remote     bool
	loops, txs []region
}
type region struct {
	pos    token.Pos
	edges  map[string]bool
	remote bool
}
type graph struct {
	fs      *token.FileSet
	nodes   map[string]*function
	files   map[string]*ast.File
	imports map[string]map[string]string
	fields  map[string]map[string]string
}

const modulePath = "github.com/davveo/lumo/"

func render(n ast.Node) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, token.NewFileSet(), n)
	return b.String()
}
func receiverType(e ast.Expr) string {
	if p, ok := e.(*ast.StarExpr); ok {
		return receiverType(p.X)
	}
	if v, ok := e.(*ast.Ident); ok {
		return v.Name
	}
	return ""
}
func (g *graph) typeName(pkg string, e ast.Expr) string {
	if p, ok := e.(*ast.StarExpr); ok {
		return g.typeName(pkg, p.X)
	}
	if p, ok := e.(*ast.CompositeLit); ok {
		return g.typeName(pkg, p.Type)
	}
	if p, ok := e.(*ast.UnaryExpr); ok {
		return g.typeName(pkg, p.X)
	}
	if v, ok := e.(*ast.Ident); ok {
		return pkg + "." + v.Name
	}
	if v, ok := e.(*ast.SelectorExpr); ok {
		if x, ok := v.X.(*ast.Ident); ok {
			if imp := g.imports[pkg][x.Name]; strings.HasPrefix(imp, modulePath) {
				return strings.TrimPrefix(imp, modulePath) + "." + v.Sel.Name
			}
		}
	}
	return ""
}
func (g *graph) exprType(f *function, e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return f.types[id.Name]
	}
	if s, ok := e.(*ast.SelectorExpr); ok {
		return g.fields[g.exprType(f, s.X)][s.Sel.Name]
	}
	return g.typeName(f.pkg, e)
}
func (g *graph) resolve(f *function, e ast.Expr) string {
	switch v := e.(type) {
	case *ast.FuncLit:
		return g.literal(f, v)
	case *ast.Ident:
		if key := f.funcs[v.Name]; key != "" {
			return key
		}
		if g.nodes[f.pkg+"."+v.Name] != nil {
			return f.pkg + "." + v.Name
		}
	case *ast.SelectorExpr:
		if typ := g.exprType(f, v.X); typ != "" {
			return typ + "." + v.Sel.Name
		}
	}
	return ""
}
func (g *graph) literal(parent *function, lit *ast.FuncLit) string {
	key := parent.key + "@" + g.fs.Position(lit.Pos()).String()
	if g.nodes[key] != nil {
		return key
	}
	f := &function{
		key:       key,
		pkg:       parent.pkg,
		body:      lit.Body,
		types:     map[string]string{},
		funcs:     map[string]string{},
		edges:     map[string]bool{},
		operation: map[string]bool{},
	}
	for k, v := range parent.types {
		f.types[k] = v
	}
	for k, v := range parent.funcs {
		f.funcs[k] = v
	}
	g.nodes[key] = f
	g.bind(f, lit.Type)
	return key
}
func (g *graph) bind(f *function, typ *ast.FuncType) {
	if typ.Params == nil {
		return
	}
	for _, field := range typ.Params.List {
		for _, name := range field.Names {
			f.params = append(f.params, name.Name)
			f.types[name.Name] = g.typeName(f.pkg, field.Type)
			if render(field.Type) == "platform.Operation" {
				f.operation[name.Name] = true
			}
		}
	}
}
func (g *graph) directRemote(f *function, call *ast.CallExpr) bool {
	if id, ok := call.Fun.(*ast.Ident); ok && f.operation[id.Name] {
		return true
	}
	text := render(call.Fun)
	if strings.Contains(text, "NewClients(") && strings.Count(text, ".") >= 3 {
		return true
	}
	if s, ok := call.Fun.(*ast.SelectorExpr); ok {
		if s.Sel.Name == "Call" &&
			(strings.Contains(text, ".rpc.") || strings.Contains(text, ".RPC.") || strings.HasSuffix(g.exprType(f, s.X), ".Caller")) {
			return true
		}
		if s.Sel.Name == "Do" || s.Sel.Name == "SignUpload" || s.Sel.Name == "StatObject" ||
			s.Sel.Name == "PresignedGetObject" {
			return true
		}
		if strings.HasSuffix(g.exprType(f, s.X), ".Clients") {
			return true
		}
		root := strings.Split(text, ".")[0]
		if f.types[root] == "rpc-clients" && strings.Count(text, ".") >= 2 {
			return true
		}
	}
	return false
}
func (g *graph) scan(f *function) {
	// Bind locals first. Receiver field types let helper calls cross repository
	// wrappers; operation parameters keep indirect function-value RPCs visible.
	ast.Inspect(f.body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			g.literal(f, lit)
			return false
		}
		a, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, left := range a.Lhs {
			if i >= len(a.Rhs) {
				continue
			}
			id, ok := left.(*ast.Ident)
			if !ok {
				continue
			}
			right := a.Rhs[i]
			if lit, ok := right.(*ast.FuncLit); ok {
				f.funcs[id.Name] = g.literal(f, lit)
			} else if strings.Contains(render(right), "platform.NewClients(") {
				f.types[id.Name] = "rpc-clients"
			} else if typ := g.typeName(f.pkg, right); typ != "" {
				f.types[id.Name] = typ
			}
			if key := g.resolve(f, right); key != "" {
				f.funcs[id.Name] = key
			}
		}
		return true
	})
	var walk func(ast.Node, []*region)
	walk = func(n ast.Node, scopes []*region) {
		if n == nil {
			return
		}
		add := func(key string, remote bool) {
			if key != "" {
				f.edges[key] = true
			}
			if remote {
				f.remote = true
			}
			for _, r := range scopes {
				if key != "" {
					r.edges[key] = true
				}
				r.remote = r.remote || remote
			}
		}
		switch v := n.(type) {
		case *ast.FuncLit:
			key := g.literal(f, v)
			add(key, false)
			return
		case *ast.RangeStmt:
			walk(v.X, scopes)
			f.loops = append(f.loops, region{pos: v.Pos(), edges: map[string]bool{}})
			r := &f.loops[len(f.loops)-1]
			walk(v.Body, append(scopes, r))
			return
		case *ast.ForStmt:
			walk(v.Init, scopes)
			f.loops = append(f.loops, region{pos: v.Pos(), edges: map[string]bool{}})
			r := &f.loops[len(f.loops)-1]
			inner := append(scopes, r)
			walk(v.Cond, inner)
			walk(v.Post, inner)
			walk(v.Body, inner)
			return
		case *ast.CallExpr:
			key := g.resolve(f, v.Fun)
			add(key, g.directRemote(f, v))
			if callee := g.nodes[key]; callee != nil {
				for i, arg := range v.Args {
					if i >= len(callee.params) {
						break
					}
					if target := g.resolve(f, arg); target != "" {
						callee.funcs[callee.params[i]] = target
					}
				}
			}
			if s, ok := v.Fun.(*ast.SelectorExpr); ok && (s.Sel.Name == "Transaction" || s.Sel.Name == "Mutate" || s.Sel.Name == "PopupTransaction" || s.Sel.Name == "Claim") {
				for _, arg := range v.Args {
					if target := g.resolve(f, arg); target != "" {
						f.txs = append(f.txs, region{pos: v.Pos(), edges: map[string]bool{target: true}})
					}
				}
			}
		}
		ast.Inspect(n, func(child ast.Node) bool {
			if child == n {
				return true
			}
			if child == nil {
				return false
			}
			walk(child, scopes)
			return false
		})
	}
	walk(f.body, nil)
}
func load(t *testing.T) *graph {
	t.Helper()
	return loadRoot(t, filepath.Clean("../.."))
}
func loadRoot(t *testing.T, root string) *graph {
	t.Helper()
	g := &graph{
		fs:      token.NewFileSet(),
		nodes:   map[string]*function{},
		files:   map[string]*ast.File{},
		imports: map[string]map[string]string{},
		fields:  map[string]map[string]string{},
	}
	for _, directory := range []string{"services", "internal"} {
		err := filepath.Walk(filepath.Join(root, directory), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "pkg" || info.Name() == ".local" || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
				strings.HasSuffix(path, ".pb.go") ||
				strings.Contains(path, "generated") {
				return nil
			}
			file, e := parser.ParseFile(g.fs, path, nil, 0)
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			pkg := filepath.ToSlash(rel)
			g.files[path] = file
			if g.imports[pkg] == nil {
				g.imports[pkg] = map[string]string{}
			}
			for _, imp := range file.Imports {
				value := strings.Trim(imp.Path.Value, "\"")
				name := filepath.Base(value)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				g.imports[pkg][name] = value
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for path, file := range g.files {
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		pkg := filepath.ToSlash(rel)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					key := pkg + "." + ts.Name.Name
					g.fields[key] = map[string]string{}
					for _, field := range st.Fields.List {
						for _, name := range field.Names {
							g.fields[key][name.Name] = g.typeName(pkg, field.Type)
						}
					}
				}
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				key := pkg + "."
				f := &function{pkg: pkg, body: d.Body, types: map[string]string{}, funcs: map[string]string{}, edges: map[string]bool{}, operation: map[string]bool{}}
				if d.Recv != nil {
					recv := d.Recv.List[0]
					typ := receiverType(recv.Type)
					key += typ + "."
					if len(recv.Names) > 0 {
						f.types[recv.Names[0].Name] = pkg + "." + typ
					}
				}
				f.key = key + d.Name.Name
				g.bind(f, d.Type)
				g.nodes[f.key] = f
			}
		}
	}
	// Literal nodes can be discovered while scanning; repeat until every body is scanned.
	scanned := map[string]bool{}
	for {
		added := false
		for key, f := range g.nodes {
			if !scanned[key] {
				g.scan(f)
				scanned[key] = true
				added = true
			}
		}
		if !added {
			break
		}
	}

	// A function argument can be bound by a caller scanned after its callee.
	// Rescan bodies after bindings stabilize so transaction wrappers and passed
	// helper values cannot hide a remote dependency.
	for pass := 0; pass < 2; pass++ {
		for _, f := range g.nodes {
			f.edges = map[string]bool{}
			f.loops = nil
			f.txs = nil
			f.remote = false
			g.scan(f)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, f := range g.nodes {
			if f.remote {
				continue
			}
			for edge := range f.edges {
				if callee := g.nodes[edge]; callee != nil && callee.remote {
					f.remote = true
					changed = true
					break
				}
			}
		}
	}
	return g
}
func (g *graph) regionRemote(r region) bool {
	if r.remote {
		return true
	}
	for edge := range r.edges {
		if n := g.nodes[edge]; n != nil && n.remote {
			return true
		}
	}
	return false
}
func TestBusinessLoopsAndTransactionsDoNotCallRemoteMethods(t *testing.T) {
	g := load(t)
	for _, f := range g.nodes {
		if strings.HasPrefix(f.pkg, "services/") || strings.HasPrefix(f.pkg, "internal/httpgateway") {
			for _, r := range f.loops {
				if g.regionRemote(r) {
					t.Errorf("RPC/remote call from loop: %s (%s)", g.fs.Position(r.pos), f.key)
				}
			}
		}
		for _, r := range f.txs {
			if g.regionRemote(r) {
				t.Errorf("RPC/remote call from transaction callback: %s (%s)", g.fs.Position(r.pos), f.key)
			}
		}
	}
}

func TestRemoteGuardDetectsIndirectHelpersAndTransactionCallbacks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "services", "fixture")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(root, "internal"), 0700); e != nil {
		t.Fatal(e)
	}
	source := `package fixture
 import "github.com/davveo/lumo/internal/platform"
 type Service struct {rpc platform.Caller}
 func(s *Service) remote(){s.rpc.Call(nil,"catalog",platform.Command{})}
 func(s *Service) helper(){s.remote()}
 func(s *Service) Loop(){for i:=0;i<2;i++{s.helper()}}
 func(s *Service) Closure(){f:=func(){s.helper()};for i:=0;i<2;i++{f()}}
 func(s *Service) Transaction(){s.repo.Transaction(nil,func(){s.helper()})}
 func(s *Service) Pure(){for i:=0;i<2;i++{_ = i}}
 func(s *Service) RangeOnce(){for range s.remoteRows() {}}
 func(s *Service) remoteRows()[]string{s.remote();return nil}
 `
	if e := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	g := loadRoot(t, root)
	for _, name := range []string{"Loop", "Closure", "Transaction"} {
		f := g.nodes["services/fixture.Service."+name]
		regions := append(append([]region{}, f.loops...), f.txs...)
		found := false
		for _, r := range regions {
			found = found || g.regionRemote(r)
		}
		if !found {
			t.Errorf("guard missed %s", name)
		}
	}
	for _, name := range []string{"Pure", "RangeOnce"} {
		for _, r := range g.nodes["services/fixture.Service."+name].loops {
			if g.regionRemote(r) {
				t.Errorf("false positive in %s", name)
			}
		}
	}
}
func TestSnapshotInventoryContainsOnlyLocalOperations(t *testing.T) {
	g := load(t)
	raw, err := os.ReadFile("../../api/mall/v1/service_methods.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Services []struct {
			Service  string                            `json:"service"`
			Constant string                            `json:"constant"`
			Methods  []struct{ Action, Method string } `json:"methods"`
		} `json:"services"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	methods := map[string]map[string]string{}
	for _, service := range manifest.Services {
		methods[service.Constant] = map[string]string{}
		for _, method := range service.Methods {
			methods[service.Constant][method.Action] = method.Method
		}
	}
	file, err := parser.ParseFile(g.fs, "../../internal/platform/read_snapshot.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		service, ok := kv.Key.(*ast.SelectorExpr)
		if !ok || methods[service.Sel.Name] == nil {
			return true
		}
		actions, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return true
		}
		serviceName := strings.TrimPrefix(service.Sel.Name, "Service")
		serviceName = strings.ToLower(serviceName)
		for _, expr := range actions.Elts {
			action, ok := expr.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			method := methods[service.Sel.Name][action.Sel.Name]
			key := "services/" + serviceName + "/internal/usecase.Service." + method
			f := g.nodes[key]
			if f == nil {
				t.Errorf("snapshot operation missing: %s/%s (%s)", serviceName, action.Sel.Name, key)
			} else if f.remote {
				t.Errorf("snapshot operation has remote dependency: %s", key)
			}
		}
		return true
	})
}
