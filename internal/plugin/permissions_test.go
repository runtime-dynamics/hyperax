package plugin

import (
	"strings"
	"testing"
)

func TestValidatePermissions_AllValid(t *testing.T) {
	perms := []string{
		"workspace:read",
		"workspace:write",
		"tools:register",
		"storage:read",
		"storage:write",
		"network:local",
		"network:*",
	}
	if err := ValidatePermissions(perms); err != nil {
		t.Errorf("ValidatePermissions() unexpected error: %v", err)
	}
}

func TestValidatePermissions_Empty(t *testing.T) {
	if err := ValidatePermissions(nil); err != nil {
		t.Errorf("ValidatePermissions(nil) unexpected error: %v", err)
	}
	if err := ValidatePermissions([]string{}); err != nil {
		t.Errorf("ValidatePermissions([]) unexpected error: %v", err)
	}
}

func TestValidatePermissions_UnknownSingle(t *testing.T) {
	err := ValidatePermissions([]string{"workspace:read", "root:access"})
	if err == nil {
		t.Fatal("ValidatePermissions() should reject unknown permission")
	}
	if !strings.Contains(err.Error(), "root:access") {
		t.Errorf("error = %q, should mention 'root:access'", err.Error())
	}
}

func TestValidatePermissions_UnknownMultiple(t *testing.T) {
	err := ValidatePermissions([]string{"bad:one", "bad:two"})
	if err == nil {
		t.Fatal("ValidatePermissions() should reject unknown permissions")
	}
	if !strings.Contains(err.Error(), "bad:one") {
		t.Errorf("error = %q, should mention 'bad:one'", err.Error())
	}
	if !strings.Contains(err.Error(), "bad:two") {
		t.Errorf("error = %q, should mention 'bad:two'", err.Error())
	}
}

func TestValidatePermissions_SubsetValid(t *testing.T) {
	if err := ValidatePermissions([]string{"tools:register"}); err != nil {
		t.Errorf("ValidatePermissions() unexpected error: %v", err)
	}
}
