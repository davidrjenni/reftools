package main

import "unsafe"

type S struct {
	a int
	b bool
	c complex64
	d uint16
	e float32
	f string
	g uintptr
	h unsafe.Pointer
}

var _ = S{}
