// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !goexperiment.jsonv2

package json

import _ "unsafe"

//go:linkname tcParseInterface
func tcParseInterface(b []byte, useNumber bool) any {
	d := &decodeState{}
	d.init(b)
	d.scan.reset()
	d.useNumber = useNumber
	d.scanWhile(scanSkipSpace)
	return d.valueInterface()
}
