// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

func init() {
	register("PinObjs", testPinObjs)
	register("GCALot", testGCALot)
	register("MemSim", testMemSim)
	register("SpecialRelocation", testSpecialRelocation)
	register("InstallFinalizerRecursion", testInstallFinalizerRecursion)
	register("MapWithGABPointerAsKey", testMapWithGABPointerAsKey)
	register("MapWithNormalObjectAsKey", testMapWithNormalObjectAsKey)
	register("GabMapAsyncPreemption", testMapAsyncPreemption)
	register("ConcGCBitsClean", testConcGCBitsClean)
	register("FindBlockFast", testFindBlockFast)
	register("GabMarkStacks", testGabMarkStacks)
	register("GabRescanGCProg", gabRescanGCProg)
}

func testPinObjs() {
	if !*gabEnabled {
		panic("gabEnabled should have been turned on")
	}

	if *gab != 2 {
		panic("only work with mode GODEBUG=gab=2")
	}

	// do GC to prevent immediate GC in below checking
	runtime.GC()

	refs := make([]unsafe.Pointer, 1)
	type st struct {
		p unsafe.Pointer
	}

	ptr := unsafe.Pointer(new(int))
	refs[0] = ptr
	if !isInGabSpan(uintptr(ptr)) {
		panic("no-scan object should be in gab span")
	}
	if isGabPinned(uintptr(ptr)) {
		dumpGABObject("object", uintptr(ptr))
		panic("object should not be pinned")
	}

	gabPin(uintptr(ptr))
	if !isGabPinned(uintptr(ptr)) {
		panic("object should not be pinned")
	}

	ptr = unsafe.Pointer(new(uintptr))
	refs[0] = ptr
	// objects within same GAB buffer should also be pinned!
	if !isGabPinned(uintptr(ptr)) {
		panic("object should not be pinned")
	}
}

func testGCALot() {
	refs := make([]unsafe.Pointer, 1)
	for i := 0; i < 1024; i++ {
		a := make([]byte, 16)
		refs = append(refs, unsafe.Pointer(&a))
		if i > 4 && i%8 == 0 {
			runtime.GC()
		}
	}
}

// this method simulates a normal user program
// to run for a given period, all the input parameters
// are obtained from environment variable 'GAB_TEST_ENV'
func testMemSim() {
	const (
		concRefsLen = 64
		gabLimit    = 128
	)

	// set up testing parameters
	var (
		concAlloc int           // number of concurrent backgroup allocating goroutines
		gcAlot    bool          // if starts a separate goroutine to trigger many GCs
		dur       time.Duration // total duration of testing, 10s by default
		ended     bool          // if testing ended
	)
	testEnvs := os.Getenv("GAB_TEST_ENV")
	fs := flag.NewFlagSet("testMemSim", flag.ExitOnError)
	fs.IntVar(&concAlloc, "c", runtime.GOMAXPROCS(0), "Number of concurrent allocators")
	fs.BoolVar(&gcAlot, "g", false, "Trigger a lot of runtime.GC() operations")
	fs.DurationVar(&dur, "d", time.Second*10, "Testing durations")
	if testEnvs != "" {
		fs.Parse(strings.Split(testEnvs, ","))
	}

	// allocate memory at given size
	alloc := func(sz int) unsafe.Pointer {
		if sz <= 0 {
			sz = 1
		}
		sl := make([]byte, sz)
		return unsafe.Pointer(&sl[0])
	}

	taskAlloc := func(id int) {
		// Use a holder to hold pointers so that GAB evacuation can happen
		holder := make(map[unsafe.Pointer]struct{})
		idx := 0
		for !ended {
			holder[alloc(rand.Int()%gabLimit)] = struct{}{}
			idx++
		}
	}

	taskGCTrigger := func() {
		for !ended {
			runtime.GC()
			time.Sleep(500 * time.Millisecond)
		}
	}

	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go taskAlloc(i)
	}

	if gcAlot {
		go taskGCTrigger()
	}

	println("Testing for", dur)

	// The controller state
	tmEnd := time.Now().Add(dur)
	for !ended {
		n := time.Now()
		if n.Before(tmEnd) {
			time.Sleep(tmEnd.Sub(n))
		} else {
			ended = true
		}
	}
}

var finalizerExecuted int
var installedFinalizer int

const Length int = 1024

type Finalizers struct {
	x        [4]int
	a        int
	neighbor *Neighbor
	y        [4]int
}

type Neighbor struct {
	x int
}

type FinalizerHolder struct {
	objs [Length]*Finalizers
}

func finalizerA(i *int) {
	finalizerExecuted++
	*i = 0
}

func finalizerB(o *Finalizers) {
	finalizerExecuted++
	// Access the object pointed by o to make sure that all the objects
	// reachable from o are marked alive.
	o.a = o.neighbor.x
}

//go:noinline
func allocAndInstallFinalizers() *Finalizers {
	o := new(Finalizers)
	o.neighbor = new(Neighbor)
	runtime.SetFinalizer(o, finalizerB)
	runtime.SetFinalizer(&o.a, finalizerA)
	runtime.SetFinalizer(&o.y[len(o.y)-1], finalizerA)
	installedFinalizer += 3
	return o
}

//go:noinline
func allocHolder() *FinalizerHolder {
	return new(FinalizerHolder)
}

func testSpecialRelocation() {
	holder := allocHolder()

	for i := 0; i < Length; i++ {
		holder.objs[i] = allocAndInstallFinalizers()
		if i%128 == 0 {
			runtime.GC()
			time.Sleep(100 * time.Millisecond)
		}
	}

	// At this point, all the finalizers should not be invoked,
	// since GAB-allocated objects are still alive.
	// However, if specials are not properly handled, i.e.,
	// still in the special block linked list of the original mspan,
	// finalizers are executed once GAB-allocated objects are relocated,
	// since the original mspan object is released.
	if finalizerExecuted != 0 {
		println("Finalizer count", finalizerExecuted)
		panic("Finalizers should not be executed.")
	}

	holder = nil
	runtime.GC()
	runtime.GC()
	time.Sleep(1 * time.Second)

	// Now some finalizers should have been executed.
	if finalizerExecuted < installedFinalizer/2 {
		println("Finalizer count", finalizerExecuted)
		panic("Finalizers should have been executed.")
	}
	println("Installed finalizers", installedFinalizer)
	println("Finalizer executed", finalizerExecuted)
}

func finalizerRecursion(o *Finalizers) {
	installedFinalizer++
	finalizerExecuted++
	runtime.SetFinalizer(o, finalizerRecursion)
}

//go:noinline
func allocAndFinalizerRecursion() *Finalizers {
	o := new(Finalizers)
	installedFinalizer++
	runtime.SetFinalizer(o, finalizerRecursion)
	return o
}

func testInstallFinalizerRecursion() {
	holder := allocHolder()

	for i := 0; i < Length; i++ {
		holder.objs[i] = allocAndFinalizerRecursion()
		if i%128 == 0 {
			runtime.GC()
			time.Sleep(100 * time.Millisecond)
		}
	}

	if finalizerExecuted != 0 {
		println("Finalizer count", finalizerExecuted)
		panic("Finalizers should not be executed.")
	}

	holder = nil
	runtime.GC()
	runtime.GC()
	time.Sleep(1 * time.Second)

	// Now some finalizers should have been executed.
	if finalizerExecuted < installedFinalizer/2 {
		println("Installed finalizers", installedFinalizer)
		println("Finalizer executed", finalizerExecuted)
		panic("Finalizers should have been executed.")
	}
	println("Installed finalizers", installedFinalizer)
	println("Finalizer executed", finalizerExecuted)
}

const FREQ = 1024

type GABObject struct {
	a int
}

//go:noinline
func allocateGABArray() *[Length]*GABObject {
	var objs [Length]*GABObject
	return &objs
}

// Pointer of GAB-allocated object as map key.
// Since GAB-allocated objects are copied during GC
// and the object address is changed,
// the hash value of the map key is also changed,
// which may cause failing to get objects from the map.
func testMapWithGABPointerAsKey() {
	var objs = allocateGABArray()
	var m = make(map[*GABObject]*GABObject)

	// Initialize an array and a map.
	for i := 0; i < Length; i++ {
		o := &GABObject{i}
		objs[i] = o
		m[o] = o
	}

	for f := 0; f < FREQ; f++ {
		// All the objects in the array should be obtained from the map.
		for i := 0; i < Length; i++ {
			o := objs[i]
			v := m[o]
			if v == nil {
				panic("Objects should be in the map.")
			}
		}
		runtime.GC()
	}
}

type Holder struct {
	p *GABObject
}

//go:noinline
func allocHolderArray() *[Length]Holder {
	var holders [Length]Holder
	return &holders
}

// Similar to testMapWithGABPointerAsKey(),
// the hash code of an object which contains
// a pointer to GAB-allocated object is also
// changed due to object relocation.
func testMapWithNormalObjectAsKey() {
	var holders = allocHolderArray()
	var m map[Holder]Holder
	m = make(map[Holder]Holder)

	genVal := func(i int) int {
		return i + 1
	}

	// Initialize an array and a map.
	for i := 0; i < Length; i++ {
		o := GABObject{genVal(i)}
		h := Holder{&o}
		holders[i] = h
		m[h] = h
	}

	for f := 0; f < FREQ; f++ {
		// All the objects in the array should be obtained from the map.
		for i := 0; i < Length; i++ {
			h := holders[i]
			v := m[h]
			if v.p == nil || v.p.a != genVal(i) {
				panic("Objects should be in the map.")
			}
		}
		runtime.GC()
	}
}

// Put objects into map and let GC preempted them to check
// whether frozenBits are correctly set.
func testMapAsyncPreemption() {
	length := 4096
	go func() {
		for {
			runtime.GC()
			time.Sleep(time.Millisecond)
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < length; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var m = allocMap()
			for j := 0; j < length*10; j++ {
				o := allocGABObject(i)
				m[o] = o
			}
		}(i)
	}
	wg.Wait()
}

//go:noinline
func allocMap() map[*GABObject]*GABObject {
	m := make(map[*GABObject]*GABObject)
	return m
}

//go:noinline
func allocGABObject(i int) *GABObject {
	return &GABObject{i}
}

var refsUsedBytes []unsafe.Pointer

// Test of concurrent GAB GC bits cleaning.
// Constantly allocating objects in GABs and triggering GC.
// Make sure that concurrent GC bits cleaning does not conflict
// with GAB object allocation.
func testConcGCBitsClean() {
	length := 4096
	refsUsedBytes = make([]unsafe.Pointer, length)
	go func() {
		for {
			runtime.GC()
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < length; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < length*10; j++ {
				buf := allocBuf()
				refsUsedBytes[i%Length] = unsafe.Pointer(&buf)
			}
		}(i)
	}
	wg.Wait()
}

//go:noinline
func allocBuf() []byte {
	return make([]byte, 100)
}

// Allocate different size of GAB objects to verify the function findBlockFast().
func testFindBlockFast() {
	holder := make([]unsafe.Pointer, 1<<20)
	min := 8
	max := 128
	rand.Seed(time.Now().UnixNano())
	getSize := func() int {
		return min + rand.Intn(max-min+1)
	}
	for i := 0; i < 100; i++ {
		for j := 0; j < cap(holder); j++ {
			buf := make([]byte, getSize())
			holder[j] = unsafe.Pointer(&buf)
		}
		runtime.GC()
		time.Sleep(100 * time.Millisecond)
	}
}

// Make GAB pointers appear in the stack to check whether stack rescanning missing GAB pointers.
func testGabMarkStacks() {
	const length = 1 << 12
	const cap = 8
	flag := true

	genRandom := func() int {
		return rand.Intn(100)
	}

	// Allocate string and integer slice.
	// The string bodies and the body of integer slice are expected
	// to be allocated in GABs.
	var strHolders [length]string
	var intHolders [length][]int

	for i := 0; i < length; i++ {
		strHolders[i] = strconv.Itoa(genRandom())

	}
	for i := 0; i < length; i++ {
		vals := make([]int, cap)
		for j := 0; j < cap; j++ {
			vals = append(vals, genRandom())
		}
		intHolders[i] = vals
	}

	go func() {
		for {
			runtime.GC()
			time.Sleep(100 * time.Millisecond)
		}
	}()

	go func() {
		time.Sleep(30 * time.Second)
		flag = false
	}()

	var wg sync.WaitGroup
	var x atomic.Int64

	// Make string and slice headers appear in the stack
	// and check whether balanced GC can discover them.
	for i := 0; i < length; i++ {
		wg.Add(1)
		i := i
		go func() {
			for flag {
				val, _ := strconv.Atoi(strHolders[i])
				x.Add(int64(val))
				time.Sleep(1 * time.Millisecond)
			}
			wg.Done()
		}()

		wg.Add(1)
		go func() {
			for flag {
				vals := intHolders[i]
				for j := 0; j < len(vals); j++ {
					x.Add(int64(vals[j]))
				}
				time.Sleep(1 * time.Millisecond)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	println(x.Load())
}

func gabRescanGCProg() {
	stop := make(chan int)
	go big2(stop)
	for i := 0; i < 3000; i++ {
		time.Sleep(10 * time.Microsecond) // let big start running
		runtime.GC()
	}
	close(stop)
}

func big2(stop chan int) int {
	n := 0
	for {
		// delay so that gc is sure to have asked for a preemption
		for i := 0; i < 1e9; i++ {
			n++
		}

		// call bigframe, which used to miss the preemption in its prologue.
		for i := 0; i < 1024; i++ {
			go bigframe2(stop)
		}

		// check if we've been asked to stop.
		select {
		case <-stop:
			return n
		}
	}
}

type scanObject struct {
	f1 *int
	f2 int
	f3 *int
}

const capacity = 1 << 14

// Object size that is larger than 128 KB falls into gcProg case.
// This threshold is determined by maxPtrmaskBytes, which is 2048.
// 2048 B = 2048 * 8 = 2^14 bits. One bit in the ptrmask represents 8 B memory,
// then maxPtrmaskBytes corresponds to 2^14 * 8 B = 2^7 KB memory.
//
// See src/cmd/compile/internal/reflectdata/reflect.go
type BigArray [capacity]scanObject

//go:noinline
func bigframe2(stop chan int) int {
	// not splitting the stack will overflow.
	// small will notice that it needs a stack split and will
	// catch the overflow.
	var x BigArray
	return small2(stop, &x)
}

func small2(stop chan int, x *BigArray) int {
	for i := range x {
		x[i].f1 = new(int)
		x[i].f2 = 0
		x[i].f3 = new(int)
	}
	sum := 0
	for i := range x {
		sum += *(x[i].f1) + x[i].f2 + *(x[i].f3)
	}

	// keep small from being a leaf function, which might
	// make it not do any stack check at all.
	nonleaf2(stop)

	return sum
}

func nonleaf2(stop chan int) bool {
	// do something that won't be inlined:
	select {
	case <-stop:
		return true
	default:
		return false
	}
}
