//go:build cgo && treesitter

// Package index — Tree-sitter based universal symbol extractor.
//
// This file provides multi-language symbol extraction using Tree-sitter CGO
// bindings. It supports Python, TypeScript, TSX, JavaScript, and Rust.
// Go files are delegated to the stdlib-based ExtractGoSymbols.
//
// Build with: go build -tags treesitter ./...
package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/hyperax/hyperax/internal/repo"
)

// langID identifies a supported language for Tree-sitter parsing.
type langID string

const (
	langPython     langID = "python"
	langTypeScript langID = "typescript"
	langTSX        langID = "tsx"
	langJavaScript langID = "javascript"
	langRust       langID = "rust"
	langGo         langID = "go"
)

// extensionToLang maps file extensions to Tree-sitter language identifiers.
var extensionToLang = map[string]langID{
	".py":  langPython,
	".pyi": langPython,
	".ts":  langTypeScript,
	".tsx": langTSX,
	".js":  langJavaScript,
	".jsx": langTSX,
	".rs":  langRust,
	".go":  langGo,
}

// languagePool caches Tree-sitter Language objects. They are thread-safe and
// can be shared across parsers.
var (
	languageCache     = make(map[langID]*tree_sitter.Language)
	languageCacheMu   sync.RWMutex
	languageCacheOnce sync.Once
)

// initLanguageCache populates the language cache with all supported grammars.
func initLanguageCache() {
	languageCacheMu.Lock()
	defer languageCacheMu.Unlock()

	languageCache[langPython] = tree_sitter.NewLanguage(tree_sitter_python.Language())
	languageCache[langTypeScript] = tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	languageCache[langTSX] = tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX())
	languageCache[langJavaScript] = tree_sitter.NewLanguage(tree_sitter_javascript.Language())
	languageCache[langRust] = tree_sitter.NewLanguage(tree_sitter_rust.Language())
}

// getLanguage returns the Tree-sitter Language for the given language ID.
func getLanguage(lang langID) (*tree_sitter.Language, error) {
	languageCacheOnce.Do(initLanguageCache)

	languageCacheMu.RLock()
	defer languageCacheMu.RUnlock()

	l, ok := languageCache[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
	return l, nil
}

// UniversalExtractor extracts symbols from source files using Tree-sitter for
// non-Go languages and the stdlib AST for Go. It is the main entry point for
// multi-language symbol extraction.
//
// Supported extensions: .py, .pyi, .ts, .tsx, .js, .jsx, .rs, .go
//
// Parameters:
//   - filePath: absolute path to the source file
//
// Returns symbols with FileID and WorkspaceID unset (caller must populate).
// Returns nil, nil for unsupported file extensions (not an error).
func UniversalExtractor(filePath string) ([]*repo.Symbol, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	lang, ok := extensionToLang[ext]
	if !ok {
		return nil, nil // unsupported extension, not an error
	}

	// Delegate Go to the stdlib parser for richer signature extraction.
	if lang == langGo {
		return ExtractGoSymbols(filePath)
	}

	return extractWithTreeSitter(filePath, lang)
}

// TreeSitterAvailable returns true when the binary was compiled with
// Tree-sitter support (the treesitter build tag was active).
func TreeSitterAvailable() bool {
	return true
}

// SupportedExtensions returns the set of file extensions the universal
// extractor can handle.
func SupportedExtensions() map[string]string {
	result := make(map[string]string, len(extensionToLang))
	for ext, lang := range extensionToLang {
		result[ext] = string(lang)
	}
	return result
}

// extractWithTreeSitter parses a source file using Tree-sitter and extracts
// top-level symbol definitions (functions, classes, methods, etc.).
func extractWithTreeSitter(filePath string, lang langID) ([]*repo.Symbol, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", filePath, err)
	}

	tsLang, err := getLanguage(lang)
	if err != nil {
		return nil, err
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(tsLang); err != nil {
		return nil, fmt.Errorf("set language %s: %w", lang, err)
	}

	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse returned nil for %s", filePath)
	}
	defer tree.Close()

	root := tree.RootNode()

	switch lang {
	case langPython:
		return extractPythonSymbols(root, src), nil
	case langTypeScript, langTSX, langJavaScript:
		return extractTypeScriptSymbols(root, src), nil
	case langRust:
		return extractRustSymbols(root, src), nil
	default:
		return nil, fmt.Errorf("no extractor for language: %s", lang)
	}
}

// --- Python symbol extraction ---

// extractPythonSymbols walks the Tree-sitter AST and extracts top-level
// functions, classes, and decorated definitions from Python source.
func extractPythonSymbols(root *tree_sitter.Node, src []byte) []*repo.Symbol {
	var symbols []*repo.Symbol
	cursor := root.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return symbols
	}

	for {
		node := cursor.Node()
		switch node.Kind() {
		case "function_definition":
			symbols = append(symbols, extractPythonFunc(node, src))
		case "class_definition":
			symbols = append(symbols, extractPythonClass(node, src)...)
		case "decorated_definition":
			// The actual def/class is a child of the decorator wrapper.
			inner := node.ChildByFieldName("definition")
			if inner != nil {
				switch inner.Kind() {
				case "function_definition":
					symbols = append(symbols, extractPythonFunc(inner, src))
				case "class_definition":
					symbols = append(symbols, extractPythonClass(inner, src)...)
				}
			}
		case "expression_statement":
			// Module-level assignments: NAME = ...
			sym := extractPythonAssignment(node, src)
			if sym != nil {
				symbols = append(symbols, sym)
			}
		}

		if !cursor.GotoNextSibling() {
			break
		}
	}

	return symbols
}

// extractPythonFunc extracts a single Python function definition.
func extractPythonFunc(node *tree_sitter.Node, src []byte) *repo.Symbol {
	name := nodeFieldText(node, "name", src)
	params := nodeFieldText(node, "parameters", src)

	retType := ""
	retNode := node.ChildByFieldName("return_type")
	if retNode != nil {
		retType = nodeText(retNode, src)
	}

	sig := "def " + name + params
	if retType != "" {
		sig += " -> " + retType
	}

	return &repo.Symbol{
		Name:      name,
		Kind:      "function",
		StartLine: int(node.StartPosition().Row) + 1,
		EndLine:   int(node.EndPosition().Row) + 1,
		Signature: sig,
	}
}

// extractPythonClass extracts a class and its methods.
func extractPythonClass(node *tree_sitter.Node, src []byte) []*repo.Symbol {
	className := nodeFieldText(node, "name", src)

	classSym := &repo.Symbol{
		Name:      className,
		Kind:      "class",
		StartLine: int(node.StartPosition().Row) + 1,
		EndLine:   int(node.EndPosition().Row) + 1,
		Signature: "class " + className,
	}

	// Check for superclasses.
	superNode := node.ChildByFieldName("superclasses")
	if superNode != nil {
		classSym.Signature = "class " + className + nodeText(superNode, src)
	}

	symbols := []*repo.Symbol{classSym}

	// Extract methods from the class body.
	body := node.ChildByFieldName("body")
	if body == nil {
		return symbols
	}

	for i := uint(0); i < body.NamedChildCount(); i++ {
		child := body.NamedChild(i)
		if child == nil {
			continue
		}

		switch child.Kind() {
		case "function_definition":
			method := extractPythonFunc(child, src)
			method.Kind = "method"
			symbols = append(symbols, method)
		case "decorated_definition":
			inner := child.ChildByFieldName("definition")
			if inner != nil && inner.Kind() == "function_definition" {
				method := extractPythonFunc(inner, src)
				method.Kind = "method"
				symbols = append(symbols, method)
			}
		}
	}

	return symbols
}

// extractPythonAssignment extracts module-level variable assignments.
func extractPythonAssignment(node *tree_sitter.Node, src []byte) *repo.Symbol {
	if node.NamedChildCount() < 1 {
		return nil
	}
	child := node.NamedChild(0)
	if child == nil {
		return nil
	}
	if child.Kind() != "assignment" {
		return nil
	}

	left := child.ChildByFieldName("left")
	if left == nil || left.Kind() != "identifier" {
		return nil
	}

	name := nodeText(left, src)
	// Skip private/dunder by convention — keep only UPPER_CASE or public names.
	if strings.HasPrefix(name, "_") && !strings.HasPrefix(name, "__") {
		return nil
	}

	return &repo.Symbol{
		Name:      name,
		Kind:      "variable",
		StartLine: int(node.StartPosition().Row) + 1,
		EndLine:   int(node.EndPosition().Row) + 1,
		Signature: name + " = ...",
	}
}

// --- TypeScript / JavaScript symbol extraction ---

// extractTypeScriptSymbols extracts functions, classes, interfaces, type
// aliases, enums, and exported variable declarations from TS/JS source.
func extractTypeScriptSymbols(root *tree_sitter.Node, src []byte) []*repo.Symbol {
	var symbols []*repo.Symbol
	cursor := root.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return symbols
	}

	for {
		node := cursor.Node()
		extracted := extractTSNode(node, src)
		symbols = append(symbols, extracted...)

		if !cursor.GotoNextSibling() {
			break
		}
	}

	return symbols
}

// extractTSNode extracts symbols from a single top-level TypeScript/JS node.
func extractTSNode(node *tree_sitter.Node, src []byte) []*repo.Symbol {
	kind := node.Kind()

	switch kind {
	case "function_declaration":
		return []*repo.Symbol{extractTSFunction(node, src)}

	case "class_declaration":
		return extractTSClass(node, src)

	case "interface_declaration":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "interface",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "interface " + name,
		}}

	case "type_alias_declaration":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "type",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "type " + name,
		}}

	case "enum_declaration":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "enum",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "enum " + name,
		}}

	case "export_statement":
		// Unwrap: export default function/class/interface/type/enum
		decl := node.ChildByFieldName("declaration")
		if decl != nil {
			return extractTSNode(decl, src)
		}
		// export { ... } — skip
		return nil

	case "lexical_declaration":
		// const/let declarations (often exported).
		return extractTSLexicalDecl(node, src)

	case "variable_declaration":
		return extractTSVarDecl(node, src)
	}

	return nil
}

// extractTSFunction extracts a function declaration.
func extractTSFunction(node *tree_sitter.Node, src []byte) *repo.Symbol {
	name := nodeFieldText(node, "name", src)
	params := nodeFieldText(node, "parameters", src)

	sig := "function " + name + params
	retType := node.ChildByFieldName("return_type")
	if retType != nil {
		sig += ": " + nodeText(retType, src)
	}

	return &repo.Symbol{
		Name:      name,
		Kind:      "function",
		StartLine: int(node.StartPosition().Row) + 1,
		EndLine:   int(node.EndPosition().Row) + 1,
		Signature: sig,
	}
}

// extractTSClass extracts a class and its methods.
func extractTSClass(node *tree_sitter.Node, src []byte) []*repo.Symbol {
	name := nodeFieldText(node, "name", src)

	classSym := &repo.Symbol{
		Name:      name,
		Kind:      "class",
		StartLine: int(node.StartPosition().Row) + 1,
		EndLine:   int(node.EndPosition().Row) + 1,
		Signature: "class " + name,
	}

	symbols := []*repo.Symbol{classSym}

	body := node.ChildByFieldName("body")
	if body == nil {
		return symbols
	}

	for i := uint(0); i < body.NamedChildCount(); i++ {
		child := body.NamedChild(i)
		if child == nil {
			continue
		}

		switch child.Kind() {
		case "method_definition":
			methodName := nodeFieldText(child, "name", src)
			params := nodeFieldText(child, "parameters", src)
			sym := &repo.Symbol{
				Name:      methodName,
				Kind:      "method",
				StartLine: int(child.StartPosition().Row) + 1,
				EndLine:   int(child.EndPosition().Row) + 1,
				Signature: methodName + params,
			}
			symbols = append(symbols, sym)

		case "public_field_definition", "property_definition":
			fieldName := nodeFieldText(child, "name", src)
			if fieldName != "" {
				symbols = append(symbols, &repo.Symbol{
					Name:      fieldName,
					Kind:      "property",
					StartLine: int(child.StartPosition().Row) + 1,
					EndLine:   int(child.EndPosition().Row) + 1,
					Signature: fieldName,
				})
			}
		}
	}

	return symbols
}

// extractTSLexicalDecl extracts const/let variable declarations.
func extractTSLexicalDecl(node *tree_sitter.Node, src []byte) []*repo.Symbol {
	var symbols []*repo.Symbol
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Kind() != "variable_declarator" {
			continue
		}
		name := nodeFieldText(child, "name", src)
		if name == "" {
			continue
		}

		// Check if the value is an arrow function.
		value := child.ChildByFieldName("value")
		kind := "variable"
		sig := name
		if value != nil && value.Kind() == "arrow_function" {
			kind = "function"
			params := nodeFieldText(value, "parameters", src)
			if params == "" {
				params = nodeFieldText(value, "parameter", src)
				if params != "" {
					params = "(" + params + ")"
				}
			}
			sig = "const " + name + " = " + params + " => ..."
		}

		symbols = append(symbols, &repo.Symbol{
			Name:      name,
			Kind:      kind,
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: sig,
		})
	}
	return symbols
}

// extractTSVarDecl extracts var declarations.
func extractTSVarDecl(node *tree_sitter.Node, src []byte) []*repo.Symbol {
	var symbols []*repo.Symbol
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Kind() != "variable_declarator" {
			continue
		}
		name := nodeFieldText(child, "name", src)
		if name == "" {
			continue
		}
		symbols = append(symbols, &repo.Symbol{
			Name:      name,
			Kind:      "variable",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "var " + name,
		})
	}
	return symbols
}

// --- Rust symbol extraction ---

// extractRustSymbols extracts functions, structs, enums, traits, impl blocks,
// type aliases, constants, and static variables from Rust source.
func extractRustSymbols(root *tree_sitter.Node, src []byte) []*repo.Symbol {
	var symbols []*repo.Symbol
	cursor := root.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return symbols
	}

	for {
		node := cursor.Node()
		extracted := extractRustNode(node, src)
		symbols = append(symbols, extracted...)

		if !cursor.GotoNextSibling() {
			break
		}
	}

	return symbols
}

// extractRustNode extracts symbols from a single top-level Rust node.
func extractRustNode(node *tree_sitter.Node, src []byte) []*repo.Symbol {
	kind := node.Kind()

	switch kind {
	case "function_item":
		return []*repo.Symbol{extractRustFunc(node, src)}

	case "struct_item":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "struct",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "struct " + name,
		}}

	case "enum_item":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "enum",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "enum " + name,
		}}

	case "trait_item":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "trait",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "trait " + name,
		}}

	case "impl_item":
		return extractRustImpl(node, src)

	case "type_item":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "type",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "type " + name,
		}}

	case "const_item":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "constant",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "const " + name,
		}}

	case "static_item":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "variable",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "static " + name,
		}}

	case "mod_item":
		name := nodeFieldText(node, "name", src)
		return []*repo.Symbol{{
			Name:      name,
			Kind:      "module",
			StartLine: int(node.StartPosition().Row) + 1,
			EndLine:   int(node.EndPosition().Row) + 1,
			Signature: "mod " + name,
		}}
	}

	return nil
}

// extractRustFunc extracts a function item.
func extractRustFunc(node *tree_sitter.Node, src []byte) *repo.Symbol {
	name := nodeFieldText(node, "name", src)
	params := nodeFieldText(node, "parameters", src)

	sig := "fn " + name + params
	retType := node.ChildByFieldName("return_type")
	if retType != nil {
		sig += " -> " + nodeText(retType, src)
	}

	return &repo.Symbol{
		Name:      name,
		Kind:      "function",
		StartLine: int(node.StartPosition().Row) + 1,
		EndLine:   int(node.EndPosition().Row) + 1,
		Signature: sig,
	}
}

// extractRustImpl extracts methods from an impl block.
func extractRustImpl(node *tree_sitter.Node, src []byte) []*repo.Symbol {
	typeName := nodeFieldText(node, "type", src)
	if typeName == "" {
		// Try the "name" field (trait impl: impl Trait for Type).
		typeName = nodeFieldText(node, "name", src)
	}

	var symbols []*repo.Symbol

	body := node.ChildByFieldName("body")
	if body == nil {
		return symbols
	}

	for i := uint(0); i < body.NamedChildCount(); i++ {
		child := body.NamedChild(i)
		if child == nil || child.Kind() != "function_item" {
			continue
		}

		method := extractRustFunc(child, src)
		method.Kind = "method"
		symbols = append(symbols, method)
	}

	return symbols
}

// --- Node text helpers ---

// nodeText returns the source text covered by a Tree-sitter node.
func nodeText(node *tree_sitter.Node, src []byte) string {
	start := node.StartByte()
	end := node.EndByte()
	if start >= uint(len(src)) || end > uint(len(src)) || start >= end {
		return ""
	}
	return string(src[start:end])
}

// nodeFieldText returns the source text of a named field child, or "" if the
// field does not exist.
func nodeFieldText(node *tree_sitter.Node, fieldName string, src []byte) string {
	child := node.ChildByFieldName(fieldName)
	if child == nil {
		return ""
	}
	return nodeText(child, src)
}
