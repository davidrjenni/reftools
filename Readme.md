# fixplurals [![Build Status](https://travis-ci.org/davidrjenni/fixplurals.svg?branch=master)](https://travis-ci.org/davidrjenni/fixplurals) [![GoDoc](https://godoc.org/github.com/davidrjenni/fixplurals?status.svg)](https://godoc.org/github.com/davidrjenni/fixplurals) [![Go Report Card](https://goreportcard.com/badge/github.com/davidrjenni/fixplurals)](https://goreportcard.com/report/github.com/davidrjenni/fixplurals)

fixplurals - remove redundant parameter and result types from function signatures

---

For example, the following function signature:
```
func fun(a string, b string) (c string, d string)
```
becomes:
```
func fun(a, b string) (c, d string)
```
after applying fixplurals.

## Installation

```
% go get github.com/davidrjenni/fixplurals
```

## Usage

```
% fixplurals [-dry] packages
```

Flags:

	-dry: changes are printed to stdout instead of rewriting the source files
