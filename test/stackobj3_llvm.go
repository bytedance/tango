// run

// Copyright 2018 The Go Authors. All rights reserved.
// Copyright (c) 2025 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-06-26.

// This is the TangoLLVM variant of stackobj3.go. runtime.KeepAlive keeps the
// otherwise empty use call observable when LLVM optimizes the program.
// This test makes sure that ambiguously live arguments work correctly.

package main

import (
	"runtime"
)

type HeapObj [8]int64

type StkObj struct {
	h *HeapObj
}

var n int
var c int = -1

func gc() {
	// encourage heap object to be collected, and have its finalizer run.
	runtime.GC()
	runtime.GC()
	runtime.GC()
	n++
}

var null StkObj

var sink *HeapObj

//go:noinline
func use(p *StkObj) {
	// Keep p observable so LLVM cannot eliminate the call.
	runtime.KeepAlive(p)
}

//go:noinline
func f(s StkObj, b bool) {
	var p *StkObj
	if b {
		p = &s
	} else {
		p = &null
	}
	// use is required here to prevent the conditional
	// code above from being executed after the first gc() call.
	use(p)
	// If b==false, h should be collected here.
	gc() // 0
	sink = p.h
	gc() // 1
	sink = nil
	// If b==true, h should be collected here.
	gc() // 2
}

func fTrue() {
	var s StkObj
	s.h = new(HeapObj)
	c = -1
	n = 0
	runtime.SetFinalizer(s.h, func(h *HeapObj) {
		// Remember at what phase the heap object was collected.
		c = n
	})
	f(s, true)
	if c != 2 {
		panic("bad liveness")
	}
}

func fFalse() {
	var s StkObj
	s.h = new(HeapObj)
	c = -1
	n = 0
	runtime.SetFinalizer(s.h, func(h *HeapObj) {
		// Remember at what phase the heap object was collected.
		c = n
	})
	f(s, false)
	if c != 0 {
		panic("bad liveness")
	}
}

func main() {
	fTrue()
	fFalse()
}
