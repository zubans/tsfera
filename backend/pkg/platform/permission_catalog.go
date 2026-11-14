package platform

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

//go:embed migration_permissions.json
var migrationPermissionsJSON []byte

// WithMigrationPermissions registers the same granular permissions used by the
// offline migrator. No imported name grants a wildcard or superadmin role.
func WithMigrationPermissions(module string, existing []PermissionDef) []PermissionDef {
	var mapping map[string][]string
	if err := json.Unmarshal(migrationPermissionsJSON, &mapping); err != nil {
		panic(err)
	}
	seen := map[string]bool{}
	for _, p := range existing {
		seen[p.Key] = true
	}
	keys := map[string]bool{}
	for _, values := range mapping {
		for _, key := range values {
			if strings.HasPrefix(key, module+".") && !seen[key] {
				keys[key] = true
			}
		}
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	for _, key := range sorted {
		parts := strings.Split(key, ".")
		existing = append(existing, PermissionDef{Key: key, Group: module, Resource: strings.Join(parts[1:len(parts)-1], "."), Action: parts[len(parts)-1], Description: key})
	}
	return existing
}

func RequireAnyPermission(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac, ok := AuthFromContext(r.Context())
			if !ok {
				WriteError(w, nil, NewUnauthorized("Требуется авторизация"))
				return
			}
			for _, perm := range perms {
				if ac.HasPermission(perm) {
					next.ServeHTTP(w, r)
					return
				}
			}
			WriteError(w, nil, NewForbidden("Недостаточно прав"))
		})
	}
}
