// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"sync/atomic"
)

var v int32

//go:noinline
func LoadInt32(addr *int32) (val int32) {
	return atomic.LoadInt32(addr)
}

//go:noinline
func StoreInt32(addr *int32, val int32) {
	atomic.StoreInt32(addr, val)
}

func main() {
	v = 0
	StoreInt32(&v, 123)
	if v != 123 {
		panic(fmt.Errorf("expected %d, got %d", 123, v))
	}
	v = 321
	if LoadInt32(&v) != 321 {
		panic(fmt.Errorf("expected %d, got %d", 321, v))
	}

}
