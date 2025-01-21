// run -gcflags=-beast=fmtrewrite

// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
)

var s = "ok"

func main() {
	myprintf("%s", s)
}

func myprintf(format string, v ...interface{}) {
	s = "not ok"
	println(fmt.Sprintf(format, v...))
}
