package auth

import "github.com/nic/paper-collect/backend/internal/model"

// Principal is the authenticated caller of one request.
// Scopes are already the intersection of the user's role and the token/key scopes.
type Principal struct {
	UserID    string
	Username  string
	Role      string
	KeyID     string // set for agent calls
	SessionID string // set for browser calls
	Scopes    map[string]bool
}

// IsAgent reports whether the call was authenticated with an API key.
func (p Principal) IsAgent() bool { return p.KeyID != "" }

// IsAdmin reports whether the owning user is an administrator.
func (p Principal) IsAdmin() bool { return p.Role == model.RoleAdmin }

// Has reports whether the principal holds scope.
func (p Principal) Has(scope string) bool { return p.Scopes[scope] }

var roleScopes = map[string][]string{
	model.RoleAdmin: model.AllScopes,
	model.RoleEditor: {
		model.ScopeMaterialsRead, model.ScopeMaterialsWrite,
		model.ScopeCollectionsRead, model.ScopeCollectionsWrite,
		model.ScopeUploadsWrite, model.ScopeJobsRead,
	},
	model.RoleViewer: {
		model.ScopeMaterialsRead, model.ScopeCollectionsRead, model.ScopeJobsRead,
	},
}

// RoleScopes returns the scopes granted to a role. Unknown roles get none.
func RoleScopes(role string) []string {
	return roleScopes[role]
}

// IsKnownScope reports whether scope is one of model.AllScopes.
func IsKnownScope(scope string) bool {
	for _, s := range model.AllScopes {
		if s == scope {
			return true
		}
	}
	return false
}

// EffectiveScopes intersects role scopes with explicitly granted key scopes.
// A nil keyScopes means the scopes come from the role alone (browser sessions).
func EffectiveScopes(role string, keyScopes []string) map[string]bool {
	allowed := map[string]bool{}
	for _, s := range RoleScopes(role) {
		allowed[s] = true
	}
	out := map[string]bool{}
	if keyScopes == nil {
		for s := range allowed {
			out[s] = true
		}
		return out
	}
	for _, s := range keyScopes {
		if allowed[s] {
			out[s] = true
		}
	}
	return out
}
