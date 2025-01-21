// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fmt

import _ "unsafe"

//go:linkname runtimeInitFmtSprint runtime.initFmtSprint
func runtimeInitFmtSprint(f func(...any) string)

func init() {
	runtimeInitFmtSprint(Sprint)
}
