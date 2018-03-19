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
// 	% fillstruct [-modified] -file=<filename> -offset=<byte offset> -line=<line number>
//
// Flags:
//
// -file:     filename
//
// -modified: read an archive of modified files from stdin
//
// -offset:   byte offset of the struct literal, optional if -line is present
//
// -line:     line number of the struct literal, optional if -offset is present
//
//
// If -offset as well as -line are present, then the tool first uses the
// more specific offset information. If there was no struct literal found
// at the given offset, then the line information is used.
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
	"go/parser"
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

// litInfo contains the information about
// a literal to fill with zero values.
type litInfo struct {
	typ       types.Type   // the base type of the literal
	name      *types.Named // name of the type or nil, e.g. for an anonymous struct type
	hideType  bool         // flag to hide the element type inside an array, slice or map literal
	isPointer bool         // true if the literal is of a pointer type
}

type filler struct {
	pkg      *types.Package
	pos      token.Pos
	lines    int
	existing map[string]*ast.KeyValueExpr
	first    bool
}

func zeroValue(pkg *types.Package, lit *ast.CompositeLit, info litInfo) (ast.Expr, int) {
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
	return f.zero(info, make([]types.Type, 0, 8)), f.lines
}

func (f *filler) zero(info litInfo, visited []types.Type) ast.Expr {
	switch t := info.typ.(type) {
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
			// Cannot create an expression for an invalid type.
			return nil
		}
	case *types.Chan:
		return &ast.Ident{Name: "nil", NamePos: f.pos}
	case *types.Interface:
		return &ast.Ident{Name: "nil", NamePos: f.pos}
	case *types.Map:
		keyTypeName, ok := typeString(f.pkg, t.Key())
		if !ok {
			return nil
		}
		valTypeName, ok := typeString(f.pkg, t.Elem())
		if !ok {
			return nil
		}
		lit := &ast.CompositeLit{
			Lbrace: f.pos,
			Type: &ast.MapType{
				Map:   f.pos,
				Key:   ast.NewIdent(keyTypeName),
				Value: ast.NewIdent(valTypeName),
			},
		}
		f.pos++
		lit.Elts = []ast.Expr{
			&ast.KeyValueExpr{
				Key:   f.zero(litInfo{typ: t.Key(), name: info.name, hideType: true}, visited),
				Colon: f.pos,
				Value: f.zero(litInfo{typ: t.Elem(), name: info.name, hideType: true}, visited),
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
		if !info.hideType {
			typeName, ok := typeString(f.pkg, t.Elem())
			if !ok {
				return nil
			}
			lit.Type = &ast.ArrayType{
				Lbrack: f.pos,
				Len:    &ast.BasicLit{Value: strconv.FormatInt(t.Len(), 10)},
				Elt:    ast.NewIdent(typeName),
			}
		}
		lit.Elts = make([]ast.Expr, 0, t.Len())
		for i := int64(0); i < t.Len(); i++ {
			f.pos++
			elemInfo := litInfo{typ: t.Elem().Underlying(), hideType: true}
			elemInfo.name, _ = t.Elem().(*types.Named)
			if v := f.zero(elemInfo, visited); v != nil {
				lit.Elts = append(lit.Elts, v)
			}
		}
		f.lines += len(lit.Elts) + 2
		f.pos++
		lit.Rbrace = f.pos
		return lit

	case *types.Named:
		if _, ok := t.Underlying().(*types.Struct); ok {
			info.name = t
		}
		info.typ = t.Underlying()
		return f.zero(info, visited)

	case *types.Pointer:
		if _, ok := t.Elem().Underlying().(*types.Struct); ok {
			info.typ = t.Elem()
			info.isPointer = true
			return f.zero(info, visited)
		}
		return &ast.Ident{Name: "nil", NamePos: f.pos}

	case *types.Struct:
		newlit := &ast.CompositeLit{Lbrace: f.pos}
		if !info.hideType && info.name != nil {
			typeName, ok := typeString(f.pkg, info.name)
			if !ok {
				return nil
			}
			newlit.Type = ast.NewIdent(typeName)
			if info.isPointer {
				newlit.Type.(*ast.Ident).Name = "&" + newlit.Type.(*ast.Ident).Name
			}
		} else if !info.hideType && info.name == nil {
			typeName, ok := typeString(f.pkg, t)
			if !ok {
				return nil
			}
			newlit.Type = ast.NewIdent(typeName)
		}

		for _, typ := range visited {
			if t == typ {
				return newlit
			}
		}
		visited = append(visited, t)

		first := f.first
		f.first = false
		lines := 0
		imported := isImported(f.pkg, info.name)

		for i := 0; i < t.NumFields(); i++ {
			field := t.Field(i)
			if kv, ok := f.existing[field.Name()]; first && ok {
				f.pos++
				lines++
				f.fixExprPos(kv)
				newlit.Elts = append(newlit.Elts, kv)
			} else if !ok && !imported || field.Exported() {
				f.pos++
				k := &ast.Ident{Name: field.Name(), NamePos: f.pos}
				if v := f.zero(litInfo{typ: field.Type(), name: nil}, visited); v != nil {
					lines++
					newlit.Elts = append(newlit.Elts, &ast.KeyValueExpr{
						Key:   k,
						Value: v,
					})
				} else {
					f.pos--
				}
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

var errNotFound = errors.New("no struct literal found at selection")

func main() {
	log.SetFlags(0)
	log.SetPrefix("fillstruct: ")

	var (
		filename = flag.String("file", "", "filename")
		modified = flag.Bool("modified", false, "read an archive of modified files from stdin")
		offset   = flag.Int("offset", 0, "byte offset of the struct literal, optional if -line is present")
		line     = flag.Int("line", 0, "line number of the struct literal, optional if -offset is present")
	)
	flag.Parse()

	if (*offset == 0 && *line == 0) || *filename == "" {
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

	if *offset > 0 {
		err = byOffset(lprog, path, pkg, *offset)
		switch err {
		case nil:
			return
		case errNotFound:
			// try to use line information
		default:
			log.Fatal(err)
		}
	}

	if *line > 0 {
		err = byLine(lprog, path, pkg, *line)
		switch err {
		case nil:
			return
		default:
			log.Fatal(err)
		}
	}

	log.Fatal(errNotFound)
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
	conf := &loader.Config{Build: ctx}
	allowErrors(conf)
	conf.ImportWithTests(pkg.ImportPath)
	return conf.Load()
}

func byOffset(lprog *loader.Program, path string, pkg *loader.PackageInfo, offset int) error {
	f, pos, err := findPos(lprog, path, offset)
	if err != nil {
		return err
	}

	lit, litInfo, err := findCompositeLit(f, pkg.Info, pos)
	if err != nil {
		return err
	}

	start := lprog.Fset.Position(lit.Pos()).Offset
	end := lprog.Fset.Position(lit.End()).Offset

	newlit, lines := zeroValue(pkg.Pkg, lit, litInfo)
	out, err := prepareOutput(newlit, lines, start, end)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode([]output{out})
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

func findCompositeLit(f *ast.File, info types.Info, pos token.Pos) (*ast.CompositeLit, litInfo, error) {
	var linfo litInfo
	path, _ := astutil.PathEnclosingInterval(f, pos, pos)
	for i, n := range path {
		if lit, ok := n.(*ast.CompositeLit); ok {
			linfo.name, _ = info.Types[lit].Type.(*types.Named)
			linfo.typ, ok = info.Types[lit].Type.Underlying().(*types.Struct)
			if !ok {
				return nil, linfo, errNotFound
			}
			if expr, ok := path[i+1].(ast.Expr); ok {
				linfo.hideType = hideType(info.Types[expr].Type)
			}
			return lit, linfo, nil
		}
	}
	return nil, linfo, errNotFound
}

func byLine(lprog *loader.Program, path string, pkg *loader.PackageInfo, line int) (err error) {
	var f *ast.File
	for _, af := range lprog.InitialPackages()[0].Files {
		if file := lprog.Fset.File(af.Pos()); file.Name() == path {
			f = af
		}
	}
	if f == nil {
		return fmt.Errorf("could not find file %q", path)
	}

	var outs []output
	var prev types.Type
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		startLine := lprog.Fset.Position(lit.Pos()).Line
		endLine := lprog.Fset.Position(lit.End()).Line
		if !(startLine <= line && line <= endLine) {
			return true
		}

		var info litInfo
		info.name, _ = pkg.Types[lit].Type.(*types.Named)
		info.typ, ok = pkg.Types[lit].Type.Underlying().(*types.Struct)
		if !ok {
			prev = pkg.Types[lit].Type.Underlying()
			err = errNotFound
			return true
		}
		info.hideType = hideType(prev)

		startOff := lprog.Fset.Position(lit.Pos()).Offset
		endOff := lprog.Fset.Position(lit.End()).Offset
		newlit, lines := zeroValue(pkg.Pkg, lit, info)

		var out output
		out, err = prepareOutput(newlit, lines, startOff, endOff)
		if err != nil {
			return false
		}
		outs = append(outs, out)
		return false
	})
	if err != nil {
		return err
	}
	if len(outs) == 0 {
		return errNotFound
	}

	for i := len(outs)/2 - 1; i >= 0; i-- {
		opp := len(outs) - 1 - i
		outs[i], outs[opp] = outs[opp], outs[i]
	}

	return json.NewEncoder(os.Stdout).Encode(outs)
}

func hideType(t types.Type) bool {
	switch t.(type) {
	case *types.Array:
		return true
	case *types.Map:
		return true
	case *types.Slice:
		return true
	default:
		return false
	}
}

type output struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Code  string `json:"code"`
}

func prepareOutput(n ast.Node, lines, start, end int) (output, error) {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, lines)
	for i := 1; i <= lines; i++ {
		file.AddLine(i)
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, n); err != nil {
		return output{}, err
	}
	return output{
		Start: start,
		End:   end,
		Code:  buf.String(),
	}, nil
}

func allowErrors(conf *loader.Config) {
	ctxt := *conf.Build
	ctxt.CgoEnabled = false
	conf.Build = &ctxt
	conf.AllowErrors = true
	conf.ParserMode = parser.AllErrors
	conf.TypeChecker.Error = func(err error) {}
}
