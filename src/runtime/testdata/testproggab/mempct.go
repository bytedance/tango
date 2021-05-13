// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

func init() {
	register("MemoryPercentLimit", testMemPctLimit)
	register("EchoMemoryPercent", echoGabMemoryPercent)
}

func testMemPctLimit() {
	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	// to make gab decision to take effects
	ballast := make([]byte, 1<<25)

	runtime.GC()
	var mstats runtime.MemStats
	if *gabMemoryPercent <= 10 || *gabMemoryPercent > 90 {
		panic(fmt.Sprintf("gabMemoryPercent is out of range: %d", *gabMemoryPercent))
	}

	refs := make([]unsafe.Pointer, 4096, 4096)
	var wg sync.WaitGroup
	wg.Add(1)
	// allocation worker
	go func() {
		defer wg.Done()
		var allocBytes uintptr
		var allocGrain int = 127
		runtime.ReadMemStats(&mstats)
		hardLimit := uintptr(mstats.HeapAlloc) * uintptr(*gabMemoryPercent) / 100
		testAllocTarget := hardLimit * 2

		// keep allocating, until exceed testAllocTarget
		var idx int
		for allocBytes < testAllocTarget {
			buf := make([]byte, allocGrain)
			refs[idx] = unsafe.Pointer(&buf)
			idx++
			if idx >= len(refs) {
				idx = 0
			}
			allocBytes += uintptr(allocGrain)
		}
	}()
	wg.Wait()

	// adaptive policy will take effects at the end of GC
	// trigger one to ensure that
	runtime.GC()
	if *gabEnabled {
		panic("gabEnabled should have been turned off by adaptive policy")
	}
	fmt.Println("gab turned off adaptively")

	// overwrite to clear all the references, and GAB should be enabled by GC again
	refs = make([]unsafe.Pointer, 1)
	runtime.GC()
	runtime.GC() // to ensure garbage from last cycle get swept timely
	if !*gabEnabled {
		panic("gabEnabled should have been turned on by adaptive policy")
	}
	fmt.Println("gab turned on adaptively")
	ballast[0] = 1
}

func echoGabMemoryPercent() {
	fmt.Printf("%d", *gabMemoryPercent)
}
