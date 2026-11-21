package build

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/jespino/go-in-go/gig"
)

const (
	exportPragma = "//gig:export"
	importPragma = "//gig:import"
)

// scanResult is what prepareSource found in a project.
type scanResult struct {
	exports []gig.Func
	imports []gig.Func
	hasMain bool
}

// prepareSource copies the project's Go files from proj to dst, collecting
// //gig:export and //gig:import functions. Imported functions are declared
// without a body by the user; they're removed from the copy and generated.
func prepareSource(proj, dst string) (*scanResult, error) {
	if err := os.RemoveAll(dst); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(proj)
	if err != nil {
		return nil, err
	}
	res := &scanResult{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(proj, name))
		if err != nil {
			return nil, err
		}
		if name == "go.mod" || name == "go.sum" {
			if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, data, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if file.Name.Name != "main" {
			return nil, fmt.Errorf("%s: a go-in-go module is a main package (found package %s)", name, file.Name.Name)
		}
		changed := false
		var decls []ast.Decl
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				decls = append(decls, d)
				continue
			}
			if fd.Recv == nil && fd.Name.Name == "main" {
				res.hasMain = true
			}
			kind, doc := pragma(fd.Doc)
			if kind == "" {
				decls = append(decls, d)
				continue
			}
			pos := fset.Position(fd.Pos())
			f, err := funcSignature(fd, doc)
			if err != nil {
				return nil, fmt.Errorf("%s: %s %s: %w", pos, kind, fd.Name.Name, err)
			}
			switch kind {
			case exportPragma:
				if fd.Body == nil {
					return nil, fmt.Errorf("%s: %s %s: exported functions need a body", pos, kind, fd.Name.Name)
				}
				res.exports = append(res.exports, f)
				decls = append(decls, d)
			case importPragma:
				if fd.Body != nil {
					return nil, fmt.Errorf("%s: %s %s: imported functions are declared without a body", pos, kind, fd.Name.Name)
				}
				res.imports = append(res.imports, f)
				changed = true // dropped: the generated file defines it
			}
		}
		if changed {
			file.Decls = decls
			// Comments of removed declarations would end up floating
			// elsewhere; the generated file carries their doc instead.
			file.Comments = keepComments(file)
			var buf bytes.Buffer
			if err := printer.Fprint(&buf, fset, file); err != nil {
				return nil, err
			}
			data = buf.Bytes()
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// keepComments returns the comment groups that are inside a remaining
// declaration or not attached to any function declaration.
func keepComments(file *ast.File) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	for _, cg := range file.Comments {
		keep := true
		if k, _ := pragma(cg); k == importPragma {
			keep = false
		}
		if keep {
			out = append(out, cg)
		}
	}
	return out
}

// pragma returns the gig pragma in a doc comment and the rest of the doc.
func pragma(doc *ast.CommentGroup) (kind, text string) {
	if doc == nil {
		return "", ""
	}
	for _, c := range doc.List {
		t := strings.TrimSpace(c.Text)
		if t == exportPragma || t == importPragma {
			kind = t
		}
	}
	if kind == "" {
		return "", ""
	}
	return kind, strings.TrimSpace(doc.Text())
}

func funcSignature(fd *ast.FuncDecl, doc string) (gig.Func, error) {
	f := gig.Func{Name: fd.Name.Name, Doc: doc, Params: []gig.Param{}, Results: []gig.Param{}}
	if fd.Recv != nil {
		return f, fmt.Errorf("methods can't cross the module boundary")
	}
	if fd.Type.TypeParams != nil {
		return f, fmt.Errorf("generic functions can't cross the module boundary")
	}
	if !ast.IsExported(f.Name) {
		return f, fmt.Errorf("the name must be exported (start with an upper-case letter)")
	}
	var err error
	if f.Params, err = fieldList(fd.Type.Params); err != nil {
		return f, err
	}
	if f.Results, err = fieldList(fd.Type.Results); err != nil {
		return f, err
	}
	for i, p := range f.Params {
		if p.Type == "error" {
			return f, fmt.Errorf("parameter %d: error is only supported as a result", i)
		}
	}
	for i, r := range f.Results {
		if r.Type == "error" && i != len(f.Results)-1 {
			return f, fmt.Errorf("error must be the last result")
		}
	}
	if f.ArgSlots() > gig.MaxArgSlots || f.RetSlots() > gig.MaxRetSlots {
		return f, fmt.Errorf("too many parameters or results")
	}
	return f, nil
}

func fieldList(fl *ast.FieldList) ([]gig.Param, error) {
	out := []gig.Param{}
	if fl == nil {
		return out, nil
	}
	for _, field := range fl.List {
		t, err := typeName(field.Type)
		if err != nil {
			return nil, err
		}
		if len(field.Names) == 0 {
			out = append(out, gig.Param{Type: t})
		}
		for _, n := range field.Names {
			name := n.Name
			if name == "_" {
				name = ""
			}
			out = append(out, gig.Param{Name: name, Type: t})
		}
	}
	return out, nil
}

func typeName(e ast.Expr) (string, error) {
	var t string
	switch x := e.(type) {
	case *ast.Ident:
		t = x.Name
		switch t {
		case "byte":
			t = "uint8"
		case "rune":
			t = "int32"
		}
	case *ast.ArrayType:
		if id, ok := x.Elt.(*ast.Ident); ok && x.Len == nil && (id.Name == "byte" || id.Name == "uint8") {
			t = "[]byte"
		}
	}
	if !gig.ValidType(t) {
		var buf bytes.Buffer
		format.Node(&buf, token.NewFileSet(), e)
		return "", fmt.Errorf("type %s can't cross the module boundary (supported: %s)", buf.String(), strings.Join(gig.Types, ", "))
	}
	return t, nil
}
