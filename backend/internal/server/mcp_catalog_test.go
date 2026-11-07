package server

import (
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
