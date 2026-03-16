package plugin

import (
	"fmt"
	"strings"
)

// AllowedPermissions maps permission identifiers to human-readable descriptions.
// This is the canonical set of permissions a plugin may request in its manifest.
var AllowedPermissions = map[string]string{
	"workspace:read":  "Read files and symbols in workspaces",
	"workspace:write": "Modify files in workspaces",
	"tools:register":  "Register new MCP tools",
	"storage:read":    "Read from plugin-private storage",
	"storage:write":   "Write to plugin-private storage",
	"network:local":   "Make HTTP requests to localhost",
	"network:*":       "Make HTTP requests to any host",
}

// ValidatePermissions checks that every permission in the requested slice is
// a recognised permission from AllowedPermissions. Returns an error listing
// all unrecognised permissions if any are found.
func ValidatePermissions(requested []string) error {
	var unknown []string
	for _, perm := range requested {
		if _, ok := AllowedPermissions[perm]; !ok {
			unknown = append(unknown, perm)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown permissions: %s", strings.Join(unknown, ", "))
	}
	return nil
}
