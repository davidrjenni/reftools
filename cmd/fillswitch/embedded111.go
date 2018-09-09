// Copyright (c) 2018 David R. Jenni. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build go1.11

package main

import "go/types"

func embeddedType(t *types.Interface, i int) types.Type {
	return t.EmbeddedType(i)
}
