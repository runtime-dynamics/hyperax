package index

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/hyperax/hyperax/internal/repo"
)

// ExtractGoSymbols parses a Go source file and returns the symbols it contains.
// Extracted symbol kinds: function, method, struct, interface, constant, variable.
//
// Parameters:
//   - filePath: absolute path to the .go file
//
// Returns a slice of Symbol pointers (with FileID and WorkspaceID left unset --
// the caller is responsible for populating those), or an error if parsing fails.
func ExtractGoSymbols(filePath string) ([]*repo.Symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("index.ExtractGoSymbols: %w", err)
	}

	var symbols []*repo.Symbol

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			sym := extractFuncDecl(fset, node)
			symbols = append(symbols, sym)
			return false // no need to descend into function body

		case *ast.GenDecl:
			extracted := extractGenDecl(fset, node)
			symbols = append(symbols, extracted...)
			return false // we already walked the specs
		}
		return true
	})

	return symbols, nil
}

// extractFuncDecl builds a Symbol from a function or method declaration.
func extractFuncDecl(fset *token.FileSet, decl *ast.FuncDecl) *repo.Symbol {
	kind := "function"
	sig := formatFuncSignature(decl)

	if decl.Recv != nil && decl.Recv.NumFields() > 0 {
		kind = "method"
	}

	startLine := fset.Position(decl.Pos()).Line
	endLine := fset.Position(decl.End()).Line

	return &repo.Symbol{
		Name:      decl.Name.Name,
		Kind:      kind,
		StartLine: startLine,
		EndLine:   endLine,
		Signature: sig,
	}
}

// formatFuncSignature produces a human-readable signature like
// "func (r *Repo) Get(id string) (Item, error)" or "func main()".
func formatFuncSignature(decl *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")

	// Receiver.
	if decl.Recv != nil && decl.Recv.NumFields() > 0 {
		b.WriteByte('(')
		field := decl.Recv.List[0]
		if len(field.Names) > 0 {
			b.WriteString(field.Names[0].Name)
			b.WriteByte(' ')
		}
		b.WriteString(formatTypeExpr(field.Type))
		b.WriteString(") ")
	}

	b.WriteString(decl.Name.Name)
	b.WriteByte('(')
	b.WriteString(formatFieldList(decl.Type.Params))
	b.WriteByte(')')

	if decl.Type.Results != nil && decl.Type.Results.NumFields() > 0 {
		results := formatFieldList(decl.Type.Results)
		if decl.Type.Results.NumFields() == 1 && !strings.Contains(results, " ") {
			b.WriteByte(' ')
			b.WriteString(results)
		} else {
			b.WriteString(" (")
			b.WriteString(results)
			b.WriteByte(')')
		}
	}

	return b.String()
}

// extractGenDecl handles type, const, and var declarations at package level.
func extractGenDecl(fset *token.FileSet, decl *ast.GenDecl) []*repo.Symbol {
	var symbols []*repo.Symbol

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			sym := extractTypeSpec(fset, s, decl)
			if sym != nil {
				symbols = append(symbols, sym)
			}

		case *ast.ValueSpec:
			extracted := extractValueSpec(fset, s, decl)
			symbols = append(symbols, extracted...)
		}
	}

	return symbols
}

// extractTypeSpec builds a Symbol from a type declaration (struct, interface, or alias).
func extractTypeSpec(fset *token.FileSet, spec *ast.TypeSpec, decl *ast.GenDecl) *repo.Symbol {
	kind := "type"
	var sig string

	switch spec.Type.(type) {
	case *ast.StructType:
		kind = "struct"
		sig = "type " + spec.Name.Name + " struct"
	case *ast.InterfaceType:
		kind = "interface"
		sig = "type " + spec.Name.Name + " interface"
	default:
		sig = "type " + spec.Name.Name + " " + formatTypeExpr(spec.Type)
	}

	startLine := fset.Position(declStart(decl, spec)).Line
	endLine := fset.Position(spec.End()).Line

	return &repo.Symbol{
		Name:      spec.Name.Name,
		Kind:      kind,
		StartLine: startLine,
		EndLine:   endLine,
		Signature: sig,
	}
}

// extractValueSpec builds symbols from const or var declarations.
// Only package-level declarations are relevant (the caller's ast.Inspect
// does not descend into function bodies for GenDecl).
func extractValueSpec(fset *token.FileSet, spec *ast.ValueSpec, decl *ast.GenDecl) []*repo.Symbol {
	kind := "variable"
	if decl.Tok == token.CONST {
		kind = "constant"
	}

	var symbols []*repo.Symbol
	for _, name := range spec.Names {
		// Skip the blank identifier.
		if name.Name == "_" {
			continue
		}

		sig := decl.Tok.String() + " " + name.Name
		if spec.Type != nil {
			sig += " " + formatTypeExpr(spec.Type)
		}

		startLine := fset.Position(name.Pos()).Line
		endLine := fset.Position(spec.End()).Line

		symbols = append(symbols, &repo.Symbol{
			Name:      name.Name,
			Kind:      kind,
			StartLine: startLine,
			EndLine:   endLine,
			Signature: sig,
		})
	}

	return symbols
}

// declStart returns the position to use as the start of a spec. For grouped
// declarations (with parentheses), this is the spec's own Pos; for standalone
// declarations, the GenDecl's Pos includes the keyword.
func declStart(decl *ast.GenDecl, spec ast.Spec) token.Pos {
	if decl.Lparen.IsValid() {
		return spec.Pos()
	}
	return decl.Pos()
}

// formatFieldList renders a parameter or result field list as a comma-separated string.
func formatFieldList(fields *ast.FieldList) string {
	if fields == nil {
		return ""
	}

	var parts []string
	for _, field := range fields.List {
		typeName := formatTypeExpr(field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, typeName)
		} else {
			for _, name := range field.Names {
				parts = append(parts, name.Name+" "+typeName)
			}
		}
	}
	return strings.Join(parts, ", ")
}

// formatTypeExpr renders a type expression as a string. Handles common cases:
// idents, selectors, pointers, arrays, slices, maps, channels, function types,
// and ellipsis (variadic) parameters.
func formatTypeExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return formatTypeExpr(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + formatTypeExpr(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + formatTypeExpr(t.Elt)
		}
		return "[...]" + formatTypeExpr(t.Elt)
	case *ast.MapType:
		return "map[" + formatTypeExpr(t.Key) + "]" + formatTypeExpr(t.Value)
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + formatTypeExpr(t.Value)
		case ast.RECV:
			return "<-chan " + formatTypeExpr(t.Value)
		default:
			return "chan " + formatTypeExpr(t.Value)
		}
	case *ast.FuncType:
		return "func(" + formatFieldList(t.Params) + ")"
	case *ast.Ellipsis:
		return "..." + formatTypeExpr(t.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{}"
	default:
		return "unknown"
	}
}
