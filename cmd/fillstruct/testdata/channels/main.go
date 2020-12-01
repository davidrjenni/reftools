package channels

import "io"

type S struct {
	a chan int
	b chan S
	c chan interface{}
	d chan io.Writer
}

var _ = S{}
