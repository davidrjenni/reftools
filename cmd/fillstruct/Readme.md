# fillstruct [![Build Status](https://travis-ci.org/davidrjenni/reftools.svg?branch=master)](https://travis-ci.org/davidrjenni/reftools) [![GoDoc](https://godoc.org/github.com/davidrjenni/reftools?status.svg)](https://godoc.org/github.com/davidrjenni/reftools/cmd/fillstruct) [![Go Report Card](https://goreportcard.com/badge/github.com/davidrjenni/reftools)](https://goreportcard.com/report/github.com/davidrjenni/reftools)

fillstruct - fills a struct literal with default values

---

For example, given the following types,
```
type User struct {
	ID   int64
	Name string
	Addr *Address
}

type Address struct {
	City   string
	ZIP    int
	LatLng [2]float64
}
```
the following struct literal
```
var frank = User{}
```
becomes:
```
var frank = User{
	ID:   0,
	Name: "",
	Addr: &Address{
		City: "",
		ZIP:  0,
		LatLng: [2]float64{
			0.0,
			0.0,
		},
	},
}
```
after applying fillstruct.

## Installation

```
% go get -u github.com/davidrjenni/reftools/cmd/fillstruct
```

## Usage

```
% fillstruct [-modified] -file=<filename> -offset=<byte offset>
```

Flags:

	-file:     filename
	-modified: read an archive of modified files from stdin
	-offset:   byte offset of the struct literal
