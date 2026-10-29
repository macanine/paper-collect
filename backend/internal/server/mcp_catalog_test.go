package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/service"
)

// Every application route used by the frontend must also be represented in MCP.
// Browser login/refresh/logout have no meaning for a directly authenticated API key.
func TestMCPCatalogCoversApplicationRoutes(t *testing.T) {
	srv, err := New(Options{Service: &service.Service{}})
	if err != nil {
		t.Fatal(err)
	}
	router := srv.Router()
	routes := map[string]bool{}
	for _, r := range router.Routes() {
		if strings.HasPrefix(r.Path, "/api/v1/") {
			routes[r.Method+" "+r.Path] = true
		}
	}
	names := map[string]bool{}
	covered := map[string]bool{}
	for _, o := range mcpOperations {
		if names[o.name] {
			t.Errorf("duplicate MCP tool %q", o.name)
		}
		names[o.name] = true
		key := o.method + " /api/v1" + o.path
		if !routes[key] {
			t.Errorf("MCP tool %s points to missing route %s", o.name, key)
		}
		covered[key] = true
		if o.scope != "" && !auth.IsKnownScope(o.scope) {
			t.Errorf("%s uses unknown scope %s", o.name, o.scope)
		}
		seen := map[string]bool{}
		for _, f := range o.fields {
			if seen[f.name] {
				t.Errorf("%s repeats field %s", o.name, f.name)
			}
			seen[f.name] = true
		}
		for _, part := range strings.Split(o.path, "/") {
			if strings.HasPrefix(part, ":") {
				name := part[1:]
				found := false
				for _, f := range o.fields {
					if f.location == "path" && f.name == name && f.required {
						found = true
					}
				}
				if !found {
					t.Errorf("%s lacks required path field %s", o.name, name)
				}
			}
		}
	}
	for route := range routes {
		if !covered[route] && route != "POST /api/v1/auth/login" && route != "POST /api/v1/auth/refresh" && route != "POST /api/v1/auth/logout" {
			t.Errorf("REST route missing MCP operation: %s", route)
		}
	}
}

func TestMCPToolSchemasDoNotShareRequiredFields(t *testing.T) {
	create, _ := findOperation("collections_create")
	update, _ := findOperation("collections_update")
	for _, f := range create.fields {
		if f.name == "title" && !f.required {
			t.Fatal("collection title must be required when creating")
		}
	}
	for _, f := range update.fields {
		if f.name == "title" && f.required {
			t.Fatal("collection title must be optional when editing")
		}
	}
	search, _ := findOperation("questions_search")
	if search.tool().Annotations["readOnlyHint"] != true {
		t.Fatal("question search is read-only")
	}
}

func handlerFieldKind(expr ast.Expr) string {
	switch typ := expr.(type) {
	case *ast.StarExpr:
		return handlerFieldKind(typ.X)
	case *ast.Ident:
		switch typ.Name {
		case "string":
			return "string"
		case "bool":
			return "boolean"
		case "int", "int64":
			return "integer"
		}
	case *ast.ArrayType:
		if name, ok := typ.Elt.(*ast.Ident); ok && name.Name == "string" {
			return "strings"
		}
		return "array"
	case *ast.MapType:
		return "object"
	}
	return "unsupported"
}

// Compare argument names and types against the HTTP handlers, so route coverage
// cannot hide a newly added filter or request-body field from MCP clients.
func TestMCPCatalogMatchesHandlerFields(t *testing.T) {
	packages, err := parser.ParseDir(token.NewFileSet(), ".", func(info fs.FileInfo) bool {
		return strings.HasPrefix(info.Name(), "handlers_") && strings.HasSuffix(info.Name(), ".go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					functions[fn.Name.Name] = fn
				}
			}
		}
	}
	srv, err := New(Options{Service: &service.Service{}})
	if err != nil {
		t.Fatal(err)
	}
	handlers := map[string]string{}
	for _, route := range srv.Router().Routes() {
		name := route.Handler
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		handlers[route.Method+" "+route.Path] = strings.TrimSuffix(name, "-fm")
	}
	for _, o := range mcpOperations {
		fn := functions[handlers[o.method+" /api/v1"+o.path]]
		if fn == nil {
			t.Errorf("%s: handler source not found", o.name)
			continue
		}
		expectedBody := map[string]string{}
		expectedQuery := map[string]bool{}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.ValueSpec:
				if len(n.Names) != 1 || n.Names[0].Name != "req" {
					break
				}
				st, ok := n.Type.(*ast.StructType)
				if !ok {
					break
				}
				for _, f := range st.Fields.List {
					if f.Tag == nil {
						continue
					}
					tag, e := strconv.Unquote(f.Tag.Value)
					if e != nil {
						t.Fatal(e)
					}
					name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
					if name != "" && name != "-" {
						expectedBody[name] = handlerFieldKind(f.Type)
					}
				}
			case *ast.CallExpr:
				switch fun := n.Fun.(type) {
				case *ast.Ident:
					if fun.Name == "pageQuery" {
						expectedQuery["page"] = true
						expectedQuery["page_size"] = true
					}
					if fun.Name == "isTrueQuery" && len(n.Args) > 1 {
						if lit, ok := n.Args[1].(*ast.BasicLit); ok {
							if name, e := strconv.Unquote(lit.Value); e == nil {
								expectedQuery[name] = true
							}
						}
					}
				case *ast.SelectorExpr:
					if receiver, ok := fun.X.(*ast.Ident); ok && receiver.Name == "c" && (fun.Sel.Name == "Query" || fun.Sel.Name == "GetQuery") && len(n.Args) > 0 {
						if lit, ok := n.Args[0].(*ast.BasicLit); ok {
							if name, e := strconv.Unquote(lit.Value); e == nil {
								expectedQuery[name] = true
							}
						}
					}
				}
			}
			return true
		})
		actualBody := map[string]string{}
		actualQuery := map[string]bool{}
		for _, f := range o.fields {
			if f.location == "body" {
				actualBody[f.name] = f.kind
			}
			if f.location == "query" {
				actualQuery[f.name] = true
			}
		}
		if o.special != "upload" && o.special != "upload_info" && !reflect.DeepEqual(actualBody, expectedBody) {
			t.Errorf("%s body fields differ: MCP %v, HTTP %v", o.name, actualBody, expectedBody)
		}
		if !reflect.DeepEqual(actualQuery, expectedQuery) {
			t.Errorf("%s query fields differ: MCP %v, HTTP %v", o.name, actualQuery, expectedQuery)
		}
	}
}

func TestMCPDocumentationListsEveryTool(t *testing.T) {
	data, err := os.ReadFile("../../../docs/MCP.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, o := range mcpOperations {
		scope := "已认证"
		if o.scope != "" {
			scope = "`" + o.scope + "`"
		}
		if !strings.Contains(doc, "| `"+o.name+"` | `"+o.method+" /api/v1"+o.path+"` | "+scope+" |") {
			t.Errorf("MCP guide is missing or misstates %s", o.name)
		}
	}
}

func TestMCPCatalogMatchesRouteScopes(t *testing.T) {
	modelSource, err := os.ReadFile("../model/model.go")
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]string{}
	for _, match := range regexp.MustCompile(`(?m)^\s*(Scope\w+)\s*=\s*"([^"]+)"`).FindAllStringSubmatch(string(modelSource), -1) {
		scopes[match[1]] = match[2]
	}
	routerSource, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	routeScopes := map[string]string{}
	pattern := regexp.MustCompile(`authGroup\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)", s\.requireScope\(model\.(Scope\w+)\)`)
	for _, match := range pattern.FindAllStringSubmatch(string(routerSource), -1) {
		routeScopes[match[1]+" "+match[2]] = scopes[match[3]]
	}
	for _, operation := range mcpOperations {
		if actual := routeScopes[operation.method+" "+operation.path]; actual != operation.scope {
			t.Errorf("%s scope differs: MCP %q, HTTP %q", operation.name, operation.scope, actual)
		}
	}
}
