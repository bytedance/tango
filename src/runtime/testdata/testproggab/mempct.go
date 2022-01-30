// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"unsafe"
)

func init() {
	register("MemoryPercentLimit", testMemPctLimit)
	register("EchoMemoryPercent", echoGabMemoryPercent)
	register("GabHardMemPctLimit", testGabHardMemPctLimit)
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

	refs := make(map[unsafe.Pointer]struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	// allocation worker
	go func() {
		defer wg.Done()
		var allocBytes uintptr
		var allocGrain int = 127
		runtime.ReadMemStats(&mstats)
		hardLimit := uintptr(mstats.NextGC) * uintptr(*gabMemoryPercent) / 100

		// keep allocating, until exceed testAllocTarget
		for allocBytes < hardLimit {
			buf := make([]byte, allocGrain)
			ptr := unsafe.Pointer(&buf)
			refs[ptr] = struct{}{}
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
	refs = make(map[unsafe.Pointer]struct{})
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

func testGabHardMemPctLimit() {
	// to make gab decision to take effects
	ballast := make([]byte, 1<<25)
	runtime.GC()

	var memLimit int
	if mem := os.Getenv("MY_MEM_LIMIT"); mem != "" {
		if n, err := strconv.Atoi(mem); err == nil {
			memLimit = n
		} else {
			panic(fmt.Sprintf("MY_MEM_LIMIT is not a valid value: %s", mem))
		}
	} else {
		panic("MY_MEM_LIMIT is not set.")
	}

	allocGrain := 128
	refs := make(map[unsafe.Pointer]struct{})
	// make GAB memory exceeds the 10% of MY_MEM_LIMIT.
	var gabHardLimit = memLimit * 10 / 100
	var gabAllocLimit = memLimit * 11 / 100
	var allocBytes int

	var wg sync.WaitGroup
	wg.Add(1)
	// allocation worker
	go func() {
		defer wg.Done()
		for allocBytes < gabAllocLimit {
			buf := make([]byte, allocGrain)
			ptr := unsafe.Pointer(&buf)
			refs[ptr] = struct{}{}
			allocBytes += allocGrain
		}
	}()
	wg.Wait()

	runtime.GC()
	runtime.GC()
	var mstats runtime.MemStats
	runtime.ReadMemStats(&mstats)
	gabSoftLimit := uintptr(mstats.NextGC) * uintptr(*gabMemoryPercent) / 100
	fmt.Printf("GAB hard memory limit %d B, GAB soft memory limit %d B\n", gabHardLimit, gabSoftLimit)
	// GAB should have been turned off.
	if *gabEnabled {
		panic("gabEnabled should have been turned off by adaptive policy")
	}

	refs = make(map[unsafe.Pointer]struct{})
	runtime.GC()
	runtime.GC()
	// GAB memory is released. GAB should have been turned on.
	if !*gabEnabled {
		panic("gabEnabled should have been turned on by adaptive policy")
	}
	ballast[0] = 1
}
