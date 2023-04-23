// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/runtime/atomic"
	"unsafe"
)

// Tango asserts internal/atomic.Int32 to be struct { int32 }
func AtomicInt32LayoutTest() {
	var ai atomic.Int32
	for i := 0; i < 16; i++ {
		ai.Store(int32(rand()))
		p := *(*int32)(unsafe.Pointer(&ai))
		if p != ai.Load() {
			panic("internal/runtime/atomic.Int32 layout changed!")
		}
	}
}
