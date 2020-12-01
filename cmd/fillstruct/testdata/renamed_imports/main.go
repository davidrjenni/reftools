package renamed_imports

import (
	goast "go/ast"
	. "io"
	_ "io"
)

type S struct {
	a *goast.Ident
	b LimitedReader
}

var _ = S{}
