// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"internal/runtime/atomic"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
)

func init() {
	register("AllocBasic", testAllocBasic)
	register("ObjFinalizer", testObjFinalizer)
	register("AllocStr", testAllocStr)
	register("AllocStrHeader", testAllocStrHeader)
	register("ReuseBuffer", testReuseBuffer)
	register("AllocZeroSizeString", testAllocZeroSizeString)
	register("MarkStrBody", testMarkStrBody)
	register("MarkStrBodyWithAllocHeader", testMarkStrBodyWithAllocHeader)
	register("MarkStrBodyWriteBarrier", testMarkStrBodyWriteBarrier)
	register("MarkStrBodyConservative", testMarkStrBodyConservative)
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
	var addrDiff uintptr = 16
	if addr1 > addr2 || addr2-addr1 > addrDiff {
		fmt.Printf("addr1 (0x%x) addr2 (0x%x) should be in same GAB\n", addr1, addr2)
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

var refs3 []unsafe.Pointer

func testAllocStr() {
	refs3 = make([]unsafe.Pointer, 4)

	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	// sequentially allocate two slice objects and two string object with different size classes.
	// when gab and gab str buffer are enabled, bodies of slice1 and slice2 will be allocated continuously in a gab buffer,
	// the bodies of str1 and str2 will be allocated continuously in a str buffer.
	// so the bodies of slice2 and str1 should not be allocated continuously, as they are allocated in different buffers.
	var (
		slice1 = make([]byte, 16)
		slice2 = make([]byte, 64)
		str1   = string(slice1)
		str2   = string(slice2)
	)

	refs3[0] = unsafe.Pointer(&(slice1[0]))
	refs3[1] = unsafe.Pointer(&(slice2[0]))

	addr1 := uintptr(refs3[0])
	addr2 := uintptr(refs3[1])
	// addr1 must reside before addr2
	var addrDiff uintptr = 16
	if addr1 > addr2 || addr2-addr1 > addrDiff {
		fmt.Printf("addr1 (0x%x) addr2 (0x%x) should be in same GAB\n", addr1, addr2)
	}

	refs3[2] = unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&str1)).Data)
	refs3[3] = unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&str2)).Data)
	addr3 := uintptr(refs3[2])
	addr4 := uintptr(refs3[3])
	// addr3 must reside before addr4
	addrDiff = 16
	if addr3 > addr4 || addr4-addr3 > addrDiff {
		fmt.Printf("addr3 (0x%x) addr4 (0x%x) should be in same GAB str buffer\n", addr3, addr4)
	}

	// addr2 must not reside before addr3
	// the string bodies should be allocated in a str buffer, while addr1 and addr2 are in the original GAB buffer
	addrDiff = 64
	if addr3-addr2 == addrDiff {
		fmt.Printf("addr2 (0x%x) addr3 (0x%x) should not be in same GAB\n", addr2, addr3)
	}

	fmt.Println("OK")
}

type scanObj struct {
	ptr1 unsafe.Pointer
	ptr2 unsafe.Pointer
}

//go:noinline
func allocScanObj() *scanObj {
	return new(scanObj)
}

var refs4 []unsafe.Pointer
var convObj1 any
var convObj2 any

func testAllocStrHeader() {
	refs4 = make([]unsafe.Pointer, 4)

	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	// sequentially allocate two string header by convTString and allocate another scan object with the size of 16.
	// when gab and gab str header buffer are enabled, string headers will be allocated continuously in a gab buffer,
	// but the extra scanObject and str2 should not be allocated continuously, as they are allocated in different buffers.
	var (
		str1 = "str1"
		str2 = "str2"
	)
	convObj1 = str1
	convObj2 = str2
	refs4[0] = unsafe.Pointer((*[2]uintptr)(unsafe.Pointer(&convObj1))[1])
	refs4[1] = unsafe.Pointer((*[2]uintptr)(unsafe.Pointer(&convObj2))[1])
	refs4[2] = unsafe.Pointer(allocScanObj())

	addr1 := uintptr(refs4[0])
	addr2 := uintptr(refs4[1])
	addr3 := uintptr(refs4[2])
	// addr1 must reside before addr2
	var addrDiff uintptr = 16
	if addr1 > addr2 || addr2-addr1 != addrDiff {
		fmt.Printf("addr1 (0x%x) addr2 (0x%x) should be in same GAB\n", addr1, addr2)
	}

	// addr3 must not reside after addr2
	// the string header should be allocated in a str header buffer, while addr3 is allocated by mallocgc1
	if addr3-addr2 == addrDiff {
		fmt.Printf("addr2 (0x%x) addr3 (0x%x) should not be in same GAB\n", addr2, addr3)
	}

	fmt.Println("OK")
}

var refs5 []unsafe.Pointer

func testReuseBuffer() {
	refs5 = make([]unsafe.Pointer, 10)
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
		bytes := new([25]byte) // bytes is supposed to escape and allocated in gab
		refs5[0] = unsafe.Pointer(bytes)
		runtime.SetFinalizer(bytes, func(v *[25]byte) {
			atomic.Xadduintptr(&cnt, 1)
		})
	}()
	wg.Wait()

	// since we have triggered GC manually at the beginning,
	// so it is unlikely to have another GC right here
	if cnt != 0 || refs5[0] == nil {
		fmt.Println("incorrect state: cnt=%d, refs5[0]=%p\n", cnt, refs5[0])
		return
	}
	refs5[0] = nil // clear the ref

	// trigger GC to collect the dead object and trigger finalizer
	runtime.GC()
	// sleep for a while to wait for finalizer to finish
	time.Sleep(3 * time.Second)

	if cnt == 1 {
		fmt.Println("cnt must not have been incremented by finalizer because the gab buffer is reused")
		return
	}

	fmt.Println("OK")
}

func testAllocZeroSizeString() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		str := ""
		data := []byte(str)
		if len(data) != 0 {
			fmt.Println("invalid byte data allocated by GAB:", data)
		}
	}()
	wg.Wait()
	fmt.Println("OK")
}

func newStringOnHeap(s string, objIndex int, bodyStateRecords []int) string {
	strPtr := new(string)
	*strPtr = s
	body := new([1025]byte)
	(*reflect.StringHeader)(unsafe.Pointer(strPtr)).Data = uintptr(unsafe.Pointer(body))
	runtime.SetFinalizer(body, func(strBody any) {
		bodyStateRecordsLock.Lock()
		defer bodyStateRecordsLock.Unlock()
		bodyStateRecords[objIndex] = bodyStateFreed
	})
	return *strPtr
}

func makeLongStr(n int) string {
	sb := strings.Builder{}
	for i := 0; i < n; i++ {
		sb.WriteByte('x')
	}
	return sb.String()
}

const (
	bodyStateFreed  = 0
	bodyStateMarked = 1
)

var bodyStateRecords = []int{0, 0}
var bodyStateRecordsLock sync.RWMutex
var refs6 []unsafe.Pointer

func testMarkStrBody() {
	refs6 = make([]unsafe.Pointer, 4)

	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	// sequentially allocate two string header by convTString and allocate another scan object with the size of 16.
	// when gab and gab str header buffer are enabled, string headers will be allocated continuously in a gab buffer,
	// but the extra scanObject and str2 should not be allocated continuously, as they are allocated in different buffers.
	var (
		indexForStr1 = 0
		indexForStr2 = 1
		// bodyLen > 1024 to make sure the body will not be allocated in gab buffer
		bodyLen = 1025
		str1    = newStringOnHeap(makeLongStr(bodyLen), indexForStr1, bodyStateRecords)
		str2    = newStringOnHeap(makeLongStr(bodyLen), indexForStr2, bodyStateRecords)
	)
	convObj1 = str1
	convObj2 = str2
	refs6[0] = unsafe.Pointer((*[2]unsafe.Pointer)(unsafe.Pointer(&convObj1))[1])
	refs6[1] = unsafe.Pointer((*[2]unsafe.Pointer)(unsafe.Pointer(&convObj2))[1])
	refs6[2] = unsafe.Pointer(allocScanObj())
	refs6[3] = unsafe.Pointer(unsafe.Add((*[2]unsafe.Pointer)(unsafe.Pointer(&convObj1))[1], 8))
	if *(*int)(refs6[3]) != bodyLen {
		fmt.Printf("*(*int)(refs6[3]) != bodyLen, refs6[3] should point to the 'len' field of the str header")
	}

	bodyStateRecords[0] = bodyStateMarked
	bodyStateRecords[1] = bodyStateMarked
	// clear all other refs to str1 and str2, only keep the refs in global refs6
	convObj1, convObj2 = nil, nil
	str1, str2 = "", ""

	addr1 := uintptr(refs6[0])
	addr2 := uintptr(refs6[1])
	addr3 := uintptr(refs6[2])
	// check addr1 and addr2 are allocated in a gab str header buffer, addr1 must reside before addr2
	var addrDiff uintptr = 16
	if addr1 > addr2 || addr2-addr1 != addrDiff {
		fmt.Printf("addr1 (0x%x) addr2 (0x%x) should be in same GAB\n", addr1, addr2)
	}

	// addr3 must not reside after addr2
	// the string header should be allocated in a str header buffer, while addr3 is allocated by mallocgc1
	if addr3-addr2 == addrDiff {
		fmt.Printf("addr2 (0x%x) addr3 (0x%x) should not be in same GAB\n", addr2, addr3)
	}

	runtime.GC()

	// release the body of str2
	refs6[1] = nil
	runtime.GC()
	runtime.GC()
	bodyStateRecordsLock.RLock()
	if bodyStateRecords[0] != bodyStateMarked {
		fmt.Printf("bodyStateRecords[0] != bodyStateMarked, the body of str1 should not have been freed\n")
	}
	if bodyStateRecords[1] != bodyStateFreed {
		fmt.Printf("bodyStateRecords[1] != bodyStateFreed, the body of str2 should have been freed\n")
	}
	bodyStateRecordsLock.RUnlock()

	// clear the data ref of str1, check whether the ref in refs6[3] pointed to the 'len' field of str1
	// can still mark the body of str1
	refs6[0] = nil
	runtime.GC()
	runtime.GC()

	bodyStateRecordsLock.RLock()
	if bodyStateRecords[0] != bodyStateMarked {
		fmt.Printf("bodyStateRecords[0] != bodyStateMarked, the body of str1 should not have been freed\n")
	}
	if bodyStateRecords[1] != bodyStateFreed {
		fmt.Printf("bodyStateRecords[1] != bodyStateFreed, the body of str2 should have been freed\n")
	}
	bodyStateRecordsLock.RUnlock()

	// release the body of str1
	refs6[3] = nil
	runtime.GC()
	runtime.GC()
	bodyStateRecordsLock.RLock()
	if bodyStateRecords[0] != bodyStateFreed {
		fmt.Printf("bodyStateRecords[0] != bodyStateFree, the body of str1 should have been freed\n")
	}
	if bodyStateRecords[1] != bodyStateFreed {
		fmt.Printf("bodyStateRecords[1] != bodyStateFreed, the body of str2 should have been freed\n")
	}
	bodyStateRecordsLock.RUnlock()
	fmt.Println("OK")
}

var refs7 []unsafe.Pointer

func testMarkStrBodyWithAllocHeader() {
	const (
		// Alloc a lot of string headers, make the gab buffer objs allocated in the same mspan,
		// and test the calculation of the string body addr with alloc header enabled.
		objNum    = 2000
		bodyLen   = 1025
		freeIndex = 1888
	)
	refs7 = make([]unsafe.Pointer, objNum)
	bodyStates := make([]int, objNum)
	var convObj any
	for i := 0; i < objNum; i++ {
		bodyStates[i] = bodyStateMarked
		str := newStringOnHeap(makeLongStr(bodyLen), i, bodyStates)
		convObj = str
		refs7[i] = unsafe.Pointer(unsafe.Add((*[2]unsafe.Pointer)(unsafe.Pointer(&convObj))[1], 8))
		if *(*int)(refs7[i]) != bodyLen {
			fmt.Printf("refs7[i] should point to the 'len' field of the str header")
		}

	}
	runtime.GC()
	runtime.GC()

	convObj = nil
	refs7[freeIndex] = nil
	runtime.GC()
	runtime.GC()

	for i := 0; i < objNum; i++ {
		if i != freeIndex {
			if bodyStates[i] != bodyStateMarked {
				fmt.Println("body not in freeIndex should not have been freed")
			}
			continue
		}
		if bodyStates[i] != bodyStateFreed {
			fmt.Println("body in freeIndex should have been freed")
		}
	}

	fmt.Println("OK")
}

func testMarkStrBodyWriteBarrier() {
	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	bodyPtr, headerPtr := makeGabStrHeaderForWBTest()
	if gabStrHeaderBodyAddrNeedsMallocHeader(bodyPtr, headerPtr) {
		fmt.Println("OK")
		return
	}
	fmt.Println("GAB string header body address calculation did not expose malloc-header adjustment")
}

var refs8 []unsafe.Pointer

func testMarkStrBodyConservative() {
	refs8 = make([]unsafe.Pointer, 2)
	bodyStates := make([]int, 2)
	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	// allocate str1 str2 in gab str header buffer
	var (
		indexForStr1 = 0
		indexForStr2 = 1
		// bodyLen > 1024 to make sure the body will not be allocated in gab buffer
		bodyLen = 1025
		str1    = newStringOnHeap(makeLongStr(bodyLen), indexForStr1, bodyStates)
		str2    = newStringOnHeap(makeLongStr(bodyLen), indexForStr2, bodyStates)
	)
	convObj1 = boxStringForTest(str1)
	convObj2 = boxStringForTest(str2)
	refs8[0] = unsafe.Pointer((*[2]unsafe.Pointer)(unsafe.Pointer(&convObj1))[1])
	refs8[1] = unsafe.Pointer((*[2]unsafe.Pointer)(unsafe.Pointer(&convObj2))[1])

	bodyStates[0] = bodyStateMarked
	bodyStates[1] = bodyStateMarked
	// clear all other refs to str1 and str2, only keep the refs in global refs8
	convObj1, convObj2 = nil, nil
	str1, str2 = "", ""

	runtime.GC()

	// release the body of str2 and record its addr in freedHeaderPointer
	freedHeaderPointer := uintptr(refs8[1])
	refs8[1] = nil
	runtime.GC()
	runtime.GC()
	bodyStateRecordsLock.RLock()
	if bodyStates[0] != bodyStateMarked {
		fmt.Printf("bodyStates[0] != bodyStateMarked, the body of str1 should not have been freed\n")
	}
	if bodyStates[1] != bodyStateFreed {
		fmt.Printf("bodyStates[1] != bodyStateFreed, the body of str2 should have been freed\n")
	}
	bodyStateRecordsLock.RUnlock()
	runtime.GC()
	// Start a goroutine in a long for loop and pass freedHeaderPointer to its stack.
	// GC will trigger asyncPreempt to it and run scanConservative on its stack
	go func(header uintptr) {
		headerPointerInStack := header
		sum := uintptr(0)

		for {
			// Do complex calculation here to prevent the compiler
			// optimizing headerPointerInStack away
			sum += headerPointerInStack * 100000000
			if sum%100000 != 0 {
				sum = headerPointerInStack / 3100000000
			}
		}
	}(freedHeaderPointer)

	// run GC to trigger asyncPreempt
	runtime.GC()
	runtime.GC()

	fmt.Println("OK")
}
