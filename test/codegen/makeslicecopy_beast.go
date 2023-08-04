// asmcheck -opt=beast

// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen

func ReadBinary(buf []byte, size int) (value []byte, length int, err error) {
	if size < 0 || size > len(buf) {
		return value, length, nil
	}
	// amd64: ".*runtime.makeslicecopy"
	// arm64: ".*runtime.makeslicecopy"
	value = make([]byte, size)
	copy(value, buf[length:length+size])
	length += size
	return
}
