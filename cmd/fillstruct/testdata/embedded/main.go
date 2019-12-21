package embedded

import (
	"go/ast"
	"io"
)

type s1 struct {
	a chan int
	b chan S
	c chan interface{}
	d chan io.Writer
	e s2
}

type s2 struct {
	*ast.Ident
}

var _ = s1{}
