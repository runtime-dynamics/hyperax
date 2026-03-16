package agentmail

import (
	"encoding/json"
	"testing"
)

func TestParseSchemaID_Valid(t *testing.T) {
	ns, name, version, err := ParseSchemaID("slack.message.v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "slack" {
		t.Fatalf("expected namespace 'slack', got %q", ns)
	}
	if name != "message" {
		t.Fatalf("expected name 'message', got %q", name)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}
}

func TestParseSchemaID_MultiPartName(t *testing.T) {
	ns, name, version, err := ParseSchemaID("agentmail.envelope.delivery.v3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "agentmail" {
		t.Fatalf("expected namespace 'agentmail', got %q", ns)
	}
	if name != "envelope.delivery" {
		t.Fatalf("expected name 'envelope.delivery', got %q", name)
	}
	if version != 3 {
		t.Fatalf("expected version 3, got %d", version)
	}
}

func TestParseSchemaID_Errors(t *testing.T) {
	cases := []struct {
		id  string
		err string
	}{
		{"", "empty schema ID"},
		{"foo", "expected namespace.name.vN"},
		{"foo.bar", "expected namespace.name.vN"},
		{"foo.bar.x1", "expected vN"},
		{"foo.bar.v0", "version must be >= 1"},
		{"foo.bar.vABC", "invalid version number"},
	}

	for _, tc := range cases {
		_, _, _, err := ParseSchemaID(tc.id)
		if err == nil {
			t.Fatalf("expected error for %q", tc.id)
		}
		if !containsStr(err.Error(), tc.err) {
			t.Fatalf("for %q: expected error containing %q, got %q", tc.id, tc.err, err.Error())
		}
	}
}

func TestSchemaKey(t *testing.T) {
	key := SchemaKey("slack", "message")
	if key != "slack.message" {
		t.Fatalf("expected 'slack.message', got %q", key)
	}
}

func TestSchemaRegistry_Register_Get(t *testing.T) {
	reg := NewSchemaRegistry()
	err := reg.Register(&SchemaVersion{
		SchemaID: "test.payload.v1",
		Fields:   map[string]string{"name": "string"},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	schema := reg.Get("test.payload.v1")
	if schema == nil {
		t.Fatal("expected schema, got nil")
	}
	if schema.Namespace != "test" {
		t.Fatalf("expected namespace 'test', got %q", schema.Namespace)
	}
	if schema.Version != 1 {
		t.Fatalf("expected version 1, got %d", schema.Version)
	}
}

func TestSchemaRegistry_Register_Duplicate(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{SchemaID: "test.payload.v1"})
	err := reg.Register(&SchemaVersion{SchemaID: "test.payload.v1"})
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestSchemaRegistry_Register_Nil(t *testing.T) {
	reg := NewSchemaRegistry()
	if err := reg.Register(nil); err == nil {
		t.Fatal("expected error for nil schema")
	}
}

func TestSchemaRegistry_Get_NotFound(t *testing.T) {
	reg := NewSchemaRegistry()
	if s := reg.Get("nonexistent.schema.v1"); s != nil {
		t.Fatal("expected nil for nonexistent schema")
	}
}

func TestSchemaRegistry_Get_InvalidID(t *testing.T) {
	reg := NewSchemaRegistry()
	if s := reg.Get("invalid"); s != nil {
		t.Fatal("expected nil for invalid schema ID")
	}
}

func TestSchemaRegistry_Latest(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{SchemaID: "test.msg.v1"})
	_ = reg.Register(&SchemaVersion{SchemaID: "test.msg.v3"})
	_ = reg.Register(&SchemaVersion{SchemaID: "test.msg.v2"})

	latest := reg.Latest("test", "msg")
	if latest == nil {
		t.Fatal("expected latest schema")
	}
	if latest.Version != 3 {
		t.Fatalf("expected version 3, got %d", latest.Version)
	}
}

func TestSchemaRegistry_Latest_NotFound(t *testing.T) {
	reg := NewSchemaRegistry()
	if s := reg.Latest("nope", "nada"); s != nil {
		t.Fatal("expected nil for nonexistent schema")
	}
}

func TestSchemaRegistry_Versions(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{SchemaID: "test.msg.v3"})
	_ = reg.Register(&SchemaVersion{SchemaID: "test.msg.v1"})
	_ = reg.Register(&SchemaVersion{SchemaID: "test.msg.v2"})

	versions := reg.Versions("test", "msg")
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Should be sorted ascending.
	if versions[0].Version != 1 || versions[1].Version != 2 || versions[2].Version != 3 {
		t.Fatalf("versions not sorted: %d, %d, %d", versions[0].Version, versions[1].Version, versions[2].Version)
	}
}

func TestSchemaRegistry_Versions_Empty(t *testing.T) {
	reg := NewSchemaRegistry()
	if v := reg.Versions("x", "y"); len(v) != 0 {
		t.Fatalf("expected empty, got %d", len(v))
	}
}

func TestSchemaRegistry_Negotiate_DirectMatch(t *testing.T) {
	reg := NewSchemaRegistry()
	result, err := reg.Negotiate("slack.message.v1", []string{"slack.message.v1", "slack.message.v2"})
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if result != "slack.message.v1" {
		t.Fatalf("expected direct match, got %q", result)
	}
}

func TestSchemaRegistry_Negotiate_Fallback(t *testing.T) {
	reg := NewSchemaRegistry()
	result, err := reg.Negotiate("slack.message.v3", []string{"slack.message.v1", "slack.message.v2"})
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	// Should fall back to highest supported version.
	if result != "slack.message.v2" {
		t.Fatalf("expected fallback to v2, got %q", result)
	}
}

func TestSchemaRegistry_Negotiate_NoMatch(t *testing.T) {
	reg := NewSchemaRegistry()
	_, err := reg.Negotiate("slack.message.v1", []string{"discord.message.v1"})
	if err == nil {
		t.Fatal("expected error for no compatible version")
	}
}

func TestSchemaRegistry_Negotiate_InvalidRequested(t *testing.T) {
	reg := NewSchemaRegistry()
	_, err := reg.Negotiate("invalid", []string{"slack.message.v1"})
	if err == nil {
		t.Fatal("expected error for invalid requested schema")
	}
}

func TestSchemaRegistry_Validate_Pass(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{
		SchemaID:       "test.msg.v1",
		Fields:         map[string]string{"text": "string", "count": "number"},
		RequiredFields: []string{"text"},
	})

	payload := json.RawMessage(`{"text": "hello", "count": 42}`)
	if err := reg.Validate("test.msg.v1", payload); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestSchemaRegistry_Validate_MissingRequired(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{
		SchemaID:       "test.msg.v1",
		Fields:         map[string]string{"text": "string"},
		RequiredFields: []string{"text"},
	})

	payload := json.RawMessage(`{"count": 42}`)
	err := reg.Validate("test.msg.v1", payload)
	if err == nil {
		t.Fatal("expected validation error for missing required field")
	}
	if !containsStr(err.Error(), "missing required fields") {
		t.Fatalf("expected 'missing required fields' error, got %q", err.Error())
	}
}

func TestSchemaRegistry_Validate_TypeMismatch(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{
		SchemaID: "test.msg.v1",
		Fields:   map[string]string{"text": "string", "count": "number"},
	})

	payload := json.RawMessage(`{"text": 123, "count": "not a number"}`)
	err := reg.Validate("test.msg.v1", payload)
	if err == nil {
		t.Fatal("expected validation error for type mismatch")
	}
	if !containsStr(err.Error(), "type mismatches") {
		t.Fatalf("expected 'type mismatches' error, got %q", err.Error())
	}
}

func TestSchemaRegistry_Validate_UnknownSchema(t *testing.T) {
	reg := NewSchemaRegistry()
	// Unknown schemas should pass validation (permissive).
	if err := reg.Validate("unknown.schema.v1", json.RawMessage(`{"any": "thing"}`)); err != nil {
		t.Fatalf("unknown schema should pass: %v", err)
	}
}

func TestSchemaRegistry_Validate_EmptyPayload(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{
		SchemaID:       "test.msg.v1",
		RequiredFields: []string{"text"},
	})

	err := reg.Validate("test.msg.v1", json.RawMessage("null"))
	if err == nil {
		t.Fatal("expected error for empty payload with required fields")
	}
}

func TestSchemaRegistry_Validate_EmptyPayload_NoRequired(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{
		SchemaID: "test.msg.v1",
	})

	if err := reg.Validate("test.msg.v1", json.RawMessage("null")); err != nil {
		t.Fatalf("should pass with no required fields: %v", err)
	}
}

func TestSchemaRegistry_Validate_InvalidJSON(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{
		SchemaID: "test.msg.v1",
		Fields:   map[string]string{"x": "string"},
	})

	err := reg.Validate("test.msg.v1", json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSchemaRegistry_List(t *testing.T) {
	reg := NewSchemaRegistry()
	_ = reg.Register(&SchemaVersion{SchemaID: "b.msg.v1"})
	_ = reg.Register(&SchemaVersion{SchemaID: "a.msg.v1"})
	_ = reg.Register(&SchemaVersion{SchemaID: "a.msg.v2"})

	ids := reg.List()
	if len(ids) != 3 {
		t.Fatalf("expected 3 schemas, got %d", len(ids))
	}
	// Should be sorted.
	if ids[0] != "a.msg.v1" || ids[1] != "a.msg.v2" || ids[2] != "b.msg.v1" {
		t.Fatalf("unexpected order: %v", ids)
	}
}

func TestSchemaRegistry_RegisterBuiltinSchemas(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.RegisterBuiltinSchemas()

	expected := []string{
		"webhook.delivery.v1",
		"slack.message.v1",
		"discord.message.v1",
		"email.message.v1",
		"agentmail.envelope.v1",
	}

	for _, sid := range expected {
		if s := reg.Get(sid); s == nil {
			t.Fatalf("expected builtin schema %q to be registered", sid)
		}
	}

	// Re-registering should not panic (idempotent).
	reg.RegisterBuiltinSchemas()
}

func TestCheckJSONType(t *testing.T) {
	cases := []struct {
		raw      string
		expected string
		ok       bool
	}{
		{`"hello"`, "string", true},
		{`42`, "number", true},
		{`-3.14`, "number", true},
		{`true`, "boolean", true},
		{`false`, "boolean", true},
		{`{"a":1}`, "object", true},
		{`[1,2]`, "array", true},
		{`null`, "null", true},
		{`"hello"`, "number", false},
		{`42`, "string", false},
		{`true`, "string", false},
		{`{"a":1}`, "array", false},
	}

	for _, tc := range cases {
		result := checkJSONType(json.RawMessage(tc.raw), tc.expected)
		if result != tc.ok {
			t.Errorf("checkJSONType(%s, %q) = %v, want %v", tc.raw, tc.expected, result, tc.ok)
		}
	}
}

// containsStr is a test helper that checks for substring presence.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
