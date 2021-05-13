// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"internal/runtime/atomic"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

func init() {
	register("AllocBasic", testAllocBasic)
	register("ObjFinalizer", testObjFinalizer)
}

// to make references escape
var refs1 []unsafe.Pointer

func testAllocBasic() {
	refs1 = make([]unsafe.Pointer, 2)

	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	// sequentially allocate two objects with different size classes
	// both beneath GAB threshold, thus
	// - in conventional mallocgc() impl, they will be in different mspan;
	// - in gab-enabled mallocgc() impl, the will be allocated continuously
	//   within the same buffer/mspan;
	refs1[0] = unsafe.Pointer(&(make([]byte, 16)[0]))
	refs1[1] = unsafe.Pointer(&(make([]byte, 64)[0]))

	addr1 := uintptr(refs1[0])
	addr2 := uintptr(refs1[1])
	// addr1 must reside before addr2
	if addr1 > addr2 || addr2-addr1 > 16 {
		fmt.Println("addr1 (0x%x) addr2 (0x%x) should be in same GAB\n", addr1, addr2)
	}
	fmt.Println("OK")
}

var refs2 []unsafe.Pointer

func testObjFinalizer() {
	refs2 = make([]unsafe.Pointer, 1)
	var cnt uintptr
	var wg sync.WaitGroup
	wg.Add(1)

	// do GC firstly to prevent an about-to-GC state
	runtime.GC()

	if !*gabEnabled {
		fmt.Println("gabEnabled should have been turned on")
		return
	}

	go func() {
		defer wg.Done()
		bytes := new([15]byte) // bytes is supposedd to escape and allocated in gab
		refs2[0] = unsafe.Pointer(bytes)
		runtime.SetFinalizer(bytes, func(v *[15]byte) {
			atomic.Xadduintptr(&cnt, 1)
		})
	}()
	wg.Wait()

	// since we have triggered GC manually at the beginning,
	// so it is unlikely to have another GC right here
	if cnt != 0 || refs2[0] == nil {
		fmt.Println("incorrect state: cnt=%d, refs2[0]=%p\n", cnt, refs2[0])
		return
	}
	refs2[0] = nil // clear the ref

	// trigger GC to collect the dead object and trigger finalizer
	runtime.GC()
	// sleep for a while to wait for finalizer to finish
	time.Sleep(3 * time.Second)

	if cnt != 1 {
		fmt.Println("cnt must have been incremented by finalizer")
		return
	}

	fmt.Println("OK")
}
