package refactor

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EnsureImports adds the specified import paths to a Go source file without
// duplicating existing imports. Each entry in imports should be either a bare
// path (e.g. "fmt") or an aliased import (e.g. "myjson encoding/json").
//
// The file is parsed with go/parser, the import declarations are inspected,
// any missing paths are added, and the file is re-formatted with go/format
// before being written back atomically.
func EnsureImports(filePath string, imports []string) error {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("refactor.EnsureImports: %w", err)
	}

	// Collect existing import paths into a set for O(1) lookup.
	existing := make(map[string]bool)
	for _, imp := range astFile.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		existing[path] = true
	}

	// Determine which imports are missing.
	var toAdd []importSpec
	for _, raw := range imports {
		spec := parseImportSpec(raw)
		if existing[spec.path] {
			continue
		}
		toAdd = append(toAdd, spec)
	}

	if len(toAdd) == 0 {
		return nil // nothing to add
	}

	// Add missing imports to the AST.
	for _, spec := range toAdd {
		addImportToAST(astFile, spec)
	}

	// Format and write back.
	return writeFormattedAST(filePath, fset, astFile)
}

// RemoveUnusedImports parses a Go file, identifies imports whose package name
// is not referenced anywhere in the file's non-import AST nodes, and removes
// them. The cleaned file is written back atomically.
func RemoveUnusedImports(filePath string) error {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("refactor.RemoveUnusedImports: %w", err)
	}

	// Collect all identifiers used in the file (excluding import specs).
	usedIdents := collectUsedIdents(astFile)

	// Walk import specs and mark unused ones for deletion.
	var removed int
	for _, decl := range astFile.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}

		var kept []ast.Spec
		for _, spec := range genDecl.Specs {
			impSpec := spec.(*ast.ImportSpec)
			name := importLocalName(impSpec)

			// Blank imports (_) and dot imports (.) are always kept.
			if name == "_" || name == "." {
				kept = append(kept, spec)
				continue
			}

			if usedIdents[name] {
				kept = append(kept, spec)
			} else {
				removed++
			}
		}
		genDecl.Specs = kept
	}

	if removed == 0 {
		return nil
	}

	// Remove any import declarations that are now empty.
	var cleanedDecls []ast.Decl
	for _, decl := range astFile.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if ok && genDecl.Tok == token.IMPORT && len(genDecl.Specs) == 0 {
			continue
		}
		cleanedDecls = append(cleanedDecls, decl)
	}
	astFile.Decls = cleanedDecls

	return writeFormattedAST(filePath, fset, astFile)
}

// importSpec holds a parsed import path with an optional alias.
type importSpec struct {
	alias string // empty string means no alias
	path  string
}

// parseImportSpec splits a raw import string into alias and path.
// Accepted formats:
//
//	"fmt"                  -> importSpec{path: "fmt"}
//	"myjson encoding/json" -> importSpec{alias: "myjson", path: "encoding/json"}
func parseImportSpec(raw string) importSpec {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) == 2 {
		return importSpec{alias: parts[0], path: parts[1]}
	}
	return importSpec{path: parts[0]}
}

// addImportToAST inserts an import spec into the AST. It appends to the first
// existing import declaration or creates a new one if none exists.
func addImportToAST(file *ast.File, spec importSpec) {
	newSpec := &ast.ImportSpec{
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: strconv.Quote(spec.path),
		},
	}
	if spec.alias != "" {
		newSpec.Name = ast.NewIdent(spec.alias)
	}

	// Try to append to an existing import block.
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		genDecl.Specs = append(genDecl.Specs, newSpec)
		// Ensure the import block uses parenthesised form.
		if !genDecl.Lparen.IsValid() {
			genDecl.Lparen = genDecl.Pos()
		}
		file.Imports = append(file.Imports, newSpec)
		return
	}

	// No existing import declaration; create one.
	newDecl := &ast.GenDecl{
		Tok:    token.IMPORT,
		Lparen: 1, // forces parenthesised form
		Specs:  []ast.Spec{newSpec},
	}

	// Insert after the package clause (before all other declarations).
	decls := make([]ast.Decl, 0, len(file.Decls)+1)
	decls = append(decls, newDecl)
	decls = append(decls, file.Decls...)
	file.Decls = decls
	file.Imports = append(file.Imports, newSpec)
}

// importLocalName returns the local name by which an import is referenced:
// the explicit alias if present, otherwise the last element of the path.
func importLocalName(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	path, _ := strconv.Unquote(spec.Path.Value)
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

// collectUsedIdents walks the AST and returns a set of all identifier names
// that appear outside of import declarations. This is used to determine which
// imports are actually referenced.
func collectUsedIdents(file *ast.File) map[string]bool {
	used := make(map[string]bool)

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			// Skip import declarations entirely.
			if node.Tok == token.IMPORT {
				return false
			}
		case *ast.SelectorExpr:
			// For x.Y, record "x" as a used identifier (package reference).
			if ident, ok := node.X.(*ast.Ident); ok {
				used[ident.Name] = true
			}
		}
		return true
	})

	return used
}

// writeFormattedAST formats the AST and writes it to the file atomically.
func writeFormattedAST(filePath string, fset *token.FileSet, file *ast.File) error {
	var buf strings.Builder
	if err := format.Node(&buf, fset, file); err != nil {
		return fmt.Errorf("format AST: %w", err)
	}

	data := []byte(buf.String())

	// Ensure parent directory exists.
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Preserve original permissions.
	perm := os.FileMode(0644)
	if info, err := os.Stat(filePath); err == nil {
		perm = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".hyperax-fmt-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename to %s: %w", filePath, err)
	}
	return nil
}
