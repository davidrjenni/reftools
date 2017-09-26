// Copyright (c) 2017 David R. Jenni. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Fillstruct fills a struct literal with default values.
//
// For example, given the following types,
//
//	type User struct {
//		ID   int64
//		Name string
//		Addr *Address
//	}
//
//	type Address struct {
//		City   string
//		ZIP    int
//		LatLng [2]float64
//	}
//
// the following struct literal
//
//	var frank = User{}
//
// becomes:
//
//	var frank = User{
//		ID:   0,
//		Name: "",
//		Addr: &Address{
//			City: "",
//			ZIP:  0,
//			LatLng: [2]float64{
//				0.0,
//				0.0,
//			},
//		},
//	}
//
// after applying fillstruct.
//
// Usage:
//
// 	% fillstruct [-modified] -file=<filename> -offset=<byte offset>
//
// Flags:
//
// -file:     filename
//
// -modified: read an archive of modified files from stdin
//
// -offset:   byte offset of the struct literal
//
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/buildutil"
	"golang.org/x/tools/go/loader"
)

type filler struct {
	pkg      *types.Package
	pos      token.Pos
	lines    int
	existing map[string]*ast.KeyValueExpr
	first    bool
}

func zeroValue(pkg *types.Package, lit *ast.CompositeLit, t *types.Struct, name *types.Named) (ast.Expr, int) {
	f := filler{
		pkg:      pkg,
		pos:      1,
		first:    true,
		existing: make(map[string]*ast.KeyValueExpr),
	}
	for _, e := range lit.Elts {
		kv := e.(*ast.KeyValueExpr)
		f.existing[kv.Key.(*ast.Ident).Name] = kv
	}
	return f.zero(t, name, false, false), f.lines
}

func (f *filler) zero(t types.Type, name *types.Named, isInArray, isPtr bool) ast.Expr {
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool:
			return &ast.Ident{Name: "false", NamePos: f.pos}
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
			return &ast.BasicLit{Value: "0", ValuePos: f.pos}
		case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
			return &ast.BasicLit{Value: "0", ValuePos: f.pos}
		case types.Uintptr:
			return &ast.BasicLit{Value: "uintptr(0)", ValuePos: f.pos}
		case types.UnsafePointer:
			return &ast.BasicLit{Value: "unsafe.Pointer(uintptr(0))", ValuePos: f.pos}
		case types.Float32, types.Float64:
			return &ast.BasicLit{Value: "0.0", ValuePos: f.pos}
		case types.Complex64, types.Complex128:
			return &ast.BasicLit{Value: "(0 + 0i)", ValuePos: f.pos}
		case types.String:
			return &ast.BasicLit{Value: `""`, ValuePos: f.pos}
		default:
			panic(fmt.Sprintf("unexpected basic type kind %v", t.Kind()))
		}
	case *types.Chan:
		return &ast.Ident{Name: "nil", NamePos: f.pos}
	case *types.Interface:
		return &ast.Ident{Name: "nil", NamePos: f.pos}
	case *types.Map:
		lit := &ast.CompositeLit{
			Lbrace: f.pos,
			Type: &ast.MapType{
				Map:   f.pos,
				Key:   ast.NewIdent(typeString(f.pkg, t.Key())),
				Value: ast.NewIdent(typeString(f.pkg, t.Elem())),
			},
		}
		f.pos++
		lit.Elts = []ast.Expr{
			&ast.KeyValueExpr{
				Key:   f.zero(t.Key(), name, true, false),
				Colon: f.pos,
				Value: f.zero(t.Elem(), name, true, false),
			},
		}
		f.pos++
		lit.Rbrace = f.pos
		f.lines += 2
		return lit
	case *types.Signature:
		return &ast.Ident{Name: "nil", NamePos: f.pos}
	case *types.Slice:
		return &ast.Ident{Name: "nil", NamePos: f.pos}

	case *types.Array:
		lit := &ast.CompositeLit{Lbrace: f.pos}
		if !isInArray {
			lit.Type = &ast.ArrayType{
				Lbrack: f.pos,
				Len:    &ast.BasicLit{Value: strconv.FormatInt(t.Len(), 10)},
				Elt:    ast.NewIdent(typeString(f.pkg, t.Elem())),
			}
		}
		lit.Elts = make([]ast.Expr, t.Len())
		for i := range lit.Elts {
			f.pos++
			n, _ := t.Elem().(*types.Named)
			lit.Elts[i] = f.zero(t.Elem().Underlying(), n, true, false)
		}
		f.lines += len(lit.Elts) + 2
		f.pos++
		lit.Rbrace = f.pos
		return lit

	case *types.Named:
		var name *types.Named
		if _, ok := t.Underlying().(*types.Struct); ok {
			name = t
		}
		return f.zero(t.Underlying(), name, isInArray, isPtr)

	case *types.Pointer:
		if _, ok := t.Elem().Underlying().(*types.Struct); ok {
			return f.zero(t.Elem(), name, isInArray, true)
		}
		return &ast.Ident{Name: "nil", NamePos: f.pos}

	case *types.Struct:
		newlit := &ast.CompositeLit{Lbrace: f.pos}
		if !isInArray && name != nil {
			newlit.Type = ast.NewIdent(typeString(f.pkg, name))
			if isPtr {
				newlit.Type.(*ast.Ident).Name = "&" + newlit.Type.(*ast.Ident).Name
			}
		} else if !isInArray && name == nil {
			newlit.Type = ast.NewIdent(typeString(f.pkg, t))
		}

		first := f.first
		f.first = false
		lines := 0
		imported := isImported(f.pkg, name)

		for i := 0; i < t.NumFields(); i++ {
			field := t.Field(i)
			if kv, ok := f.existing[field.Name()]; first && ok {
				f.pos++
				lines++
				f.fixExprPos(kv)
				newlit.Elts = append(newlit.Elts, kv)
			} else if !ok && !imported || field.Exported() {
				f.pos++
				lines++
				newlit.Elts = append(newlit.Elts, &ast.KeyValueExpr{
					Key:   &ast.Ident{Name: field.Name(), NamePos: f.pos},
					Value: f.zero(field.Type(), nil, false, false),
				})
			}
		}
		if lines > 0 {
			f.lines += lines + 2
			f.pos++
		}
		newlit.Rbrace = f.pos
		return newlit

	default:
		panic(fmt.Sprintf("unexpected type %T", t))
	}
}

func (f *filler) fixExprPos(expr ast.Expr) {
	switch expr := expr.(type) {
	case nil:
		// ignore
	case *ast.BasicLit:
		expr.ValuePos = f.pos
	case *ast.BinaryExpr:
		f.fixExprPos(expr.X)
		expr.OpPos = f.pos
		f.fixExprPos(expr.Y)
	case *ast.CallExpr:
		f.fixExprPos(expr.Fun)
		expr.Lparen = f.pos
		for _, arg := range expr.Args {
			f.fixExprPos(arg)
		}
		expr.Rparen = f.pos
	case *ast.CompositeLit:
		f.fixExprPos(expr.Type)
		expr.Lbrace = f.pos
		for _, e := range expr.Elts {
			f.pos++
			f.fixExprPos(e)
		}
		if l := len(expr.Elts); l > 0 {
			f.lines += l + 2
		}
		f.pos++
		expr.Rbrace = f.pos
	case *ast.Ellipsis:
		expr.Ellipsis = f.pos
	case *ast.FuncLit:
		expr.Type.Func = f.pos
	case *ast.Ident:
		expr.NamePos = f.pos
	case *ast.IndexExpr:
		f.fixExprPos(expr.X)
		expr.Lbrack = f.pos
		f.fixExprPos(expr.Index)
		expr.Rbrack = f.pos
	case *ast.KeyValueExpr:
		f.fixExprPos(expr.Key)
		f.fixExprPos(expr.Value)
	case *ast.ParenExpr:
		expr.Lparen = f.pos
	case *ast.SelectorExpr:
		f.fixExprPos(expr.X)
		expr.Sel.NamePos = f.pos
	case *ast.SliceExpr:
		f.fixExprPos(expr.X)
		expr.Lbrack = f.pos
		f.fixExprPos(expr.Low)
		f.fixExprPos(expr.High)
		f.fixExprPos(expr.Max)
		expr.Rbrack = f.pos
	case *ast.StarExpr:
		expr.Star = f.pos
		f.fixExprPos(expr.X)
	case *ast.UnaryExpr:
		expr.OpPos = f.pos
		f.fixExprPos(expr.X)
	}
}

func isImported(pkg *types.Package, n *types.Named) bool {
	return n != nil && pkg != n.Obj().Pkg()
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("fillstruct: ")

	var (
		filename = flag.String("file", "", "filename")
		modified = flag.Bool("modified", false, "read an archive of modified files from stdin")
		offset   = flag.Int("offset", 0, "byte offset of the struct literal")
	)
	flag.Parse()

	if *offset == 0 || *filename == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	path, err := absPath(*filename)
	if err != nil {
		log.Fatal(err)
	}

	lprog, err := load(path, *modified)
	if err != nil {
		log.Fatal(err)
	}
	pkg := lprog.InitialPackages()[0]

	f, pos, err := findPos(lprog, path, *offset)
	if err != nil {
		log.Fatal(err)
	}

	lit, typ, name, err := findCompositeLit(f, pkg.Info, pos)
	if err != nil {
		log.Fatal(err)
	}

	start := lprog.Fset.Position(lit.Pos()).Offset
	end := lprog.Fset.Position(lit.End()).Offset

	newlit, lines := zeroValue(pkg.Pkg, lit, typ, name)
	if err := print(newlit, lines, start, end); err != nil {
		log.Fatal(err)
	}
}

func absPath(filename string) (string, error) {
	eval, err := filepath.EvalSymlinks(filename)
	if err != nil {
		return "", err
	}
	return filepath.Abs(eval)
}

func load(path string, modified bool) (*loader.Program, error) {
	ctx := &build.Default
	if modified {
		archive, err := buildutil.ParseOverlayArchive(os.Stdin)
		if err != nil {
			return nil, err
		}
		ctx = buildutil.OverlayContext(ctx, archive)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	pkg, err := buildutil.ContainingPackage(ctx, cwd, path)
	if err != nil {
		return nil, err
	}
	conf := &loader.Config{
		Build: ctx,
		TypeCheckFuncBodies: func(s string) bool {
			return s == pkg.ImportPath || s == pkg.ImportPath+"_test"
		},
	}
	conf.ImportWithTests(pkg.ImportPath)
	return conf.Load()
}

func findPos(lprog *loader.Program, filename string, off int) (*ast.File, token.Pos, error) {
	for _, f := range lprog.InitialPackages()[0].Files {
		if file := lprog.Fset.File(f.Pos()); file.Name() == filename {
			if off > file.Size() {
				return nil, 0,
					fmt.Errorf("file size (%d) is smaller than given offset (%d)",
						file.Size(), off)
			}
			return f, file.Pos(off), nil
		}
	}

	return nil, 0, fmt.Errorf("could not find file %q", filename)
}

func findCompositeLit(f *ast.File, info types.Info, pos token.Pos) (*ast.CompositeLit, *types.Struct, *types.Named, error) {
	path, _ := astutil.PathEnclosingInterval(f, pos, pos)
	for _, n := range path {
		if lit, ok := n.(*ast.CompositeLit); ok {
			name, _ := info.Types[lit].Type.(*types.Named)
			typ, ok := info.Types[lit].Type.Underlying().(*types.Struct)
			if !ok {
				return nil, nil, nil, errors.New("no struct literal found at selection")
			}
			return lit, typ, name, nil
		}
	}
	return nil, nil, nil, errors.New("no struct literal found at selection")
}

func print(n ast.Node, lines, start, end int) error {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, lines)
	for i := 1; i <= lines; i++ {
		file.AddLine(i)
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, n); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Start int    `json:"start"`
		End   int    `json:"end"`
		Code  string `json:"code"`
	}{Start: start, End: end, Code: buf.String()})
}
