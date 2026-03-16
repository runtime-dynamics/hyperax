//go:build cgo && treesitter

package index

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a temporary file and returns its absolute path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// --- Python tests ---

func TestPython_Functions(t *testing.T) {
	dir := t.TempDir()
	src := `def hello():
    pass

def add(a: int, b: int) -> int:
    return a + b
`
	path := writeFile(t, dir, "funcs.py", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(symbols))
	}

	if symbols[0].Name != "hello" {
		t.Errorf("symbol 0 name = %q, want hello", symbols[0].Name)
	}
	if symbols[0].Kind != "function" {
		t.Errorf("symbol 0 kind = %q, want function", symbols[0].Kind)
	}
	if symbols[0].Signature != "def hello()" {
		t.Errorf("symbol 0 signature = %q, want %q", symbols[0].Signature, "def hello()")
	}

	if symbols[1].Name != "add" {
		t.Errorf("symbol 1 name = %q, want add", symbols[1].Name)
	}
	if symbols[1].Signature != "def add(a: int, b: int) -> int" {
		t.Errorf("symbol 1 signature = %q, want %q", symbols[1].Signature, "def add(a: int, b: int) -> int")
	}
}

func TestPython_ClassWithMethods(t *testing.T) {
	dir := t.TempDir()
	src := `class Server:
    def __init__(self, host: str, port: int):
        self.host = host
        self.port = port

    def start(self):
        pass

    @staticmethod
    def default_port() -> int:
        return 8080
`
	path := writeFile(t, dir, "server.py", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Expect: Server (class), __init__ (method), start (method), default_port (method)
	if len(symbols) != 4 {
		t.Fatalf("expected 4 symbols, got %d", len(symbols))
	}

	if symbols[0].Name != "Server" {
		t.Errorf("symbol 0 name = %q, want Server", symbols[0].Name)
	}
	if symbols[0].Kind != "class" {
		t.Errorf("symbol 0 kind = %q, want class", symbols[0].Kind)
	}

	// Methods.
	for i := 1; i < len(symbols); i++ {
		if symbols[i].Kind != "method" {
			t.Errorf("symbol %d kind = %q, want method", i, symbols[i].Kind)
		}
	}

	if symbols[1].Name != "__init__" {
		t.Errorf("symbol 1 name = %q, want __init__", symbols[1].Name)
	}
}

func TestPython_ClassInheritance(t *testing.T) {
	dir := t.TempDir()
	src := `class Base:
    pass

class Child(Base):
    pass
`
	path := writeFile(t, dir, "inherit.py", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) < 2 {
		t.Fatalf("expected at least 2 symbols, got %d", len(symbols))
	}

	if symbols[0].Name != "Base" {
		t.Errorf("symbol 0 name = %q, want Base", symbols[0].Name)
	}
	if symbols[1].Name != "Child" {
		t.Errorf("symbol 1 name = %q, want Child", symbols[1].Name)
	}
	if symbols[1].Signature != "class Child(Base)" {
		t.Errorf("symbol 1 signature = %q, want %q", symbols[1].Signature, "class Child(Base)")
	}
}

func TestPython_ModuleVariable(t *testing.T) {
	dir := t.TempDir()
	src := `MAX_RETRIES = 5
DEFAULT_HOST = "localhost"
`
	path := writeFile(t, dir, "config.py", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(symbols))
	}
	if symbols[0].Name != "MAX_RETRIES" {
		t.Errorf("symbol 0 name = %q, want MAX_RETRIES", symbols[0].Name)
	}
	if symbols[0].Kind != "variable" {
		t.Errorf("symbol 0 kind = %q, want variable", symbols[0].Kind)
	}
}

// --- TypeScript tests ---

func TestTypeScript_FunctionsAndTypes(t *testing.T) {
	dir := t.TempDir()
	src := `function greet(name: string): string {
    return "Hello, " + name;
}

interface User {
    id: number;
    name: string;
}

type ID = string | number;

enum Status {
    Active,
    Inactive,
}
`
	path := writeFile(t, dir, "types.ts", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) < 4 {
		t.Fatalf("expected at least 4 symbols, got %d", len(symbols))
	}

	nameKinds := make(map[string]string)
	for _, s := range symbols {
		nameKinds[s.Name] = s.Kind
	}

	if nameKinds["greet"] != "function" {
		t.Errorf("greet kind = %q, want function", nameKinds["greet"])
	}
	if nameKinds["User"] != "interface" {
		t.Errorf("User kind = %q, want interface", nameKinds["User"])
	}
	if nameKinds["ID"] != "type" {
		t.Errorf("ID kind = %q, want type", nameKinds["ID"])
	}
	if nameKinds["Status"] != "enum" {
		t.Errorf("Status kind = %q, want enum", nameKinds["Status"])
	}
}

func TestTypeScript_ClassWithMethods(t *testing.T) {
	dir := t.TempDir()
	src := `class Server {
    constructor(private port: number) {}

    start(): void {
        console.log("starting");
    }

    stop(): void {
        console.log("stopping");
    }
}
`
	path := writeFile(t, dir, "server.ts", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) < 3 {
		t.Fatalf("expected at least 3 symbols (class + 2+ methods), got %d", len(symbols))
	}

	if symbols[0].Name != "Server" {
		t.Errorf("symbol 0 name = %q, want Server", symbols[0].Name)
	}
	if symbols[0].Kind != "class" {
		t.Errorf("symbol 0 kind = %q, want class", symbols[0].Kind)
	}

	// Verify at least one method exists.
	hasMethod := false
	for _, s := range symbols[1:] {
		if s.Kind == "method" {
			hasMethod = true
			break
		}
	}
	if !hasMethod {
		t.Error("expected at least one method in class, found none")
	}
}

func TestTypeScript_ExportedFunction(t *testing.T) {
	dir := t.TempDir()
	src := `export function fetchData(url: string): Promise<Response> {
    return fetch(url);
}
`
	path := writeFile(t, dir, "api.ts", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "fetchData" {
		t.Errorf("name = %q, want fetchData", symbols[0].Name)
	}
	if symbols[0].Kind != "function" {
		t.Errorf("kind = %q, want function", symbols[0].Kind)
	}
}

func TestTypeScript_ArrowFunction(t *testing.T) {
	dir := t.TempDir()
	src := `const add = (a: number, b: number): number => a + b;
`
	path := writeFile(t, dir, "arrow.ts", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "add" {
		t.Errorf("name = %q, want add", symbols[0].Name)
	}
	if symbols[0].Kind != "function" {
		t.Errorf("kind = %q, want function (arrow fn detected)", symbols[0].Kind)
	}
}

// --- JavaScript tests ---

func TestJavaScript_Functions(t *testing.T) {
	dir := t.TempDir()
	src := `function hello() {
    return "hello";
}

class MyApp {
    run() {
        console.log("running");
    }
}
`
	path := writeFile(t, dir, "app.js", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) < 2 {
		t.Fatalf("expected at least 2 symbols, got %d", len(symbols))
	}

	nameKinds := make(map[string]string)
	for _, s := range symbols {
		nameKinds[s.Name] = s.Kind
	}

	if nameKinds["hello"] != "function" {
		t.Errorf("hello kind = %q, want function", nameKinds["hello"])
	}
	if nameKinds["MyApp"] != "class" {
		t.Errorf("MyApp kind = %q, want class", nameKinds["MyApp"])
	}
}

// --- Rust tests ---

func TestRust_FunctionAndStruct(t *testing.T) {
	dir := t.TempDir()
	src := `fn main() {
    println!("hello");
}

fn add(a: i32, b: i32) -> i32 {
    a + b
}

struct Config {
    host: String,
    port: u16,
}
`
	path := writeFile(t, dir, "main.rs", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(symbols))
	}

	nameKinds := make(map[string]string)
	for _, s := range symbols {
		nameKinds[s.Name] = s.Kind
	}

	if nameKinds["main"] != "function" {
		t.Errorf("main kind = %q, want function", nameKinds["main"])
	}
	if nameKinds["add"] != "function" {
		t.Errorf("add kind = %q, want function", nameKinds["add"])
	}
	if nameKinds["Config"] != "struct" {
		t.Errorf("Config kind = %q, want struct", nameKinds["Config"])
	}
}

func TestRust_TraitAndEnum(t *testing.T) {
	dir := t.TempDir()
	src := `trait Drawable {
    fn draw(&self);
}

enum Color {
    Red,
    Green,
    Blue,
}

const MAX_SIZE: usize = 1024;
`
	path := writeFile(t, dir, "shapes.rs", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	nameKinds := make(map[string]string)
	for _, s := range symbols {
		nameKinds[s.Name] = s.Kind
	}

	if nameKinds["Drawable"] != "trait" {
		t.Errorf("Drawable kind = %q, want trait", nameKinds["Drawable"])
	}
	if nameKinds["Color"] != "enum" {
		t.Errorf("Color kind = %q, want enum", nameKinds["Color"])
	}
	if nameKinds["MAX_SIZE"] != "constant" {
		t.Errorf("MAX_SIZE kind = %q, want constant", nameKinds["MAX_SIZE"])
	}
}

func TestRust_ImplMethods(t *testing.T) {
	dir := t.TempDir()
	src := `struct Server {
    port: u16,
}

impl Server {
    fn new(port: u16) -> Self {
        Server { port }
    }

    fn start(&self) {
        println!("starting on {}", self.port);
    }
}
`
	path := writeFile(t, dir, "server.rs", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Expect: Server (struct), new (method), start (method)
	if len(symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(symbols))
	}

	if symbols[0].Name != "Server" || symbols[0].Kind != "struct" {
		t.Errorf("symbol 0: %s/%s, want Server/struct", symbols[0].Name, symbols[0].Kind)
	}

	methodCount := 0
	for _, s := range symbols {
		if s.Kind == "method" {
			methodCount++
		}
	}
	if methodCount != 2 {
		t.Errorf("expected 2 methods, got %d", methodCount)
	}
}

func TestRust_ModuleAndStaticVar(t *testing.T) {
	dir := t.TempDir()
	src := `mod utils {
    pub fn helper() {}
}

static GLOBAL: &str = "hello";
`
	path := writeFile(t, dir, "lib.rs", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	nameKinds := make(map[string]string)
	for _, s := range symbols {
		nameKinds[s.Name] = s.Kind
	}

	if nameKinds["utils"] != "module" {
		t.Errorf("utils kind = %q, want module", nameKinds["utils"])
	}
	if nameKinds["GLOBAL"] != "variable" {
		t.Errorf("GLOBAL kind = %q, want variable", nameKinds["GLOBAL"])
	}
}

// --- Cross-cutting tests ---

func TestUniversalExtractor_DelegatesToGoParser(t *testing.T) {
	dir := t.TempDir()
	src := `package example

func Hello() string {
    return "hello"
}
`
	path := writeFile(t, dir, "main.go", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "Hello" {
		t.Errorf("name = %q, want Hello", symbols[0].Name)
	}
	if symbols[0].Signature != "func Hello() string" {
		t.Errorf("signature = %q, want %q", symbols[0].Signature, "func Hello() string")
	}
}

func TestUniversalExtractor_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "data.csv", "a,b,c\n1,2,3\n")

	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if symbols != nil {
		t.Errorf("expected nil for unsupported extension, got %d symbols", len(symbols))
	}
}

func TestTreeSitterAvailable(t *testing.T) {
	if !TreeSitterAvailable() {
		t.Error("TreeSitterAvailable() should return true when built with treesitter tag")
	}
}

func TestSupportedExtensions(t *testing.T) {
	exts := SupportedExtensions()

	expected := []string{".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".go"}
	for _, ext := range expected {
		if _, ok := exts[ext]; !ok {
			t.Errorf("missing extension %s", ext)
		}
	}
}

func TestPython_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "empty.py", "")

	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for empty file, got %d", len(symbols))
	}
}

func TestPython_LineNumbers(t *testing.T) {
	dir := t.TempDir()
	src := `# comment line 1
# comment line 2

def first():
    pass

def second():
    pass
`
	path := writeFile(t, dir, "lines.py", src)
	symbols, err := UniversalExtractor(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(symbols))
	}

	// first() starts on line 4.
	if symbols[0].StartLine != 4 {
		t.Errorf("first start line = %d, want 4", symbols[0].StartLine)
	}
	// second() starts on line 7.
	if symbols[1].StartLine != 7 {
		t.Errorf("second start line = %d, want 7", symbols[1].StartLine)
	}
}
