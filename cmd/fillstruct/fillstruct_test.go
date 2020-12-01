// Copyright (c) 2019 David R. Jenni. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestFillstruct(t *testing.T) {
	tests := [...]struct {
		path   []string // path to file under testdata/
		offset int      // byte offset
		line   int      // line number
		err    error    // expected error
	}{
		{path: []string{"basic", "main.go"}, offset: 145, err: errNotFound},
		{path: []string{"basic", "main.go"}, offset: 147},
		{path: []string{"basic", "main.go"}, offset: 148},
		{path: []string{"basic", "main.go"}, line: 1, err: errNotFound},
		{path: []string{"basic", "main.go"}, line: 16},
		{path: []string{"basic", "main.go"}, line: 17, err: errNotFound},
		{path: []string{"channels", "main.go"}, line: 12},
		{path: []string{"embedded", "main.go"}, line: 20},
		{path: []string{"renamed_imports", "main.go"}, line: 14},
		{path: []string{"subpkg", "main.go"}, line: 5},
		{path: []string{"two", "main.go"}, line: 15},
		{path: []string{"xtest", "pkg_test.go"}, line: 5},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.path[0], func(t *testing.T) {
			t.Parallel()

			tc.path = append([]string{"testdata"}, tc.path...)
			filename := filepath.Join(tc.path...)

			outs, err := fillstruct(filename, false, tc.offset, tc.line)
			if err != tc.err {
				t.Errorf("expected '%v', got '%v'", tc.err, err)
				return
			}
			if err != nil && err == tc.err {
				return // Don't check the output, if the errors match.
			}

			var actual bytes.Buffer
			for _, out := range outs {
				actual.WriteString(out.Code)
				actual.WriteString("\n")
			}

			golden := filepath.Join(append(tc.path[:len(tc.path)-1], "output.golden")...)
			if *update {
				if err := ioutil.WriteFile(golden, actual.Bytes(), 0644); err != nil {
					t.Fatalf("cannot update golden file: %v", err)
				}
			}

			expected, err := ioutil.ReadFile(golden)
			if err != nil {
				t.Fatalf("cannot read golden file: %v", err)
			}

			if !bytes.Equal(actual.Bytes(), expected) {
				t.Errorf("expected\n%s\ngot\n%s\n", string(expected), actual.String())
			}
		})
	}
}
