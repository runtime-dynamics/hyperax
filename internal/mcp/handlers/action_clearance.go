package handlers

import (
	"context"
	"fmt"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/pkg/types"
)

// checkActionClearance verifies that the caller has sufficient ABAC clearance
// for the given action within a consolidated tool. Each consolidated tool
// registers with the minimum clearance across all its actions; this function
// enforces per-action clearance after dispatch.
//
// Returns nil if the caller has sufficient clearance, or an error describing
// the required clearance level.
func checkActionClearance(ctx context.Context, action string, clearanceMap map[string]int) error {
	required, ok := clearanceMap[action]
	if !ok {
		return fmt.Errorf("unknown action %q", action)
	}

	auth := mcp.AuthFromContext(ctx)
	if auth.ClearanceLevel < required {
		return fmt.Errorf("insufficient clearance for action %q: requires %s (level %d), caller has level %d",
			action, types.ClearanceTierName(required), required, auth.ClearanceLevel)
	}

	return nil
}
