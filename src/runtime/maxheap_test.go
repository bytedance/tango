// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"
	"time"
	_ "unsafe"
)

var (
	setGCPercentBallast interface{}
	setGCPercentSink    interface{}
)

// GCPolicy reports the garbage collector's policy for controlling the
// heap size and scheduling garbage collection work.
type GCPolicy struct {
	GCPercent    int
	MaxHeapBytes int64
	HeapGoal     uint64
}

// More memory is managed in SetMemoryLimit, NextGc is no longer equal to the value of limit,
// if non-GC-managed memory increases, NextGc will decrease.
// Set an error value that is allowed in the test case.
const availErr = 70

// ReadGCPolicy reads the garbage collector's current policy for
// managing the heap size. This includes static settings controlled by
// the application and dynamic policy determined by heap usage.
func ReadGCPolicy(gcp *GCPolicy) {
	gcp.GCPercent, gcp.MaxHeapBytes, gcp.HeapGoal = runtime.GcReadPolicy()
}

// Note: should sync with src/runtime/mgc.go.
const maxOvershoot = 1.1

// Enable SetMaxHeap by api.
func TestSetMaxHeap(t *testing.T) {
	defer func() {
		setGCPercentBallast = nil
		setGCPercentSink = nil
		runtime.SetMaxHeap(math.MaxInt64)
		runtime.GC()
	}()

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Create 50 MB of live heap as a baseline.
	var baseline uint64 = 50 << 20
	if baseline > m1.Alloc {
		setGCPercentBallast = make([]byte, baseline-m1.Alloc)
	} else {
		baseline = m1.Alloc
	}
	// Set max heap to 2x baseline.
	var limit = int64(2 * baseline)
	prev := runtime.SetMaxHeap(limit)

	// Test return value.
	if prev != math.MaxInt64 {
		t.Errorf("want previous limit %d, got %d", ^uintptr(0), prev)
	}
	prev = runtime.SetMaxHeap(limit)
	if prev != limit {
		t.Errorf("want previous limit %d, got %d", limit, prev)
	}

	runtime.ReadMemStats(&m1)
	var m2 runtime.MemStats
	var gcp GCPolicy
	for i := 0; i < 200; i++ {
		setGCPercentSink = make([]byte, 1<<20)
		runtime.ReadMemStats(&m2)
		if m2.HeapAlloc > uint64(float64(limit)*maxOvershoot) {
			t.Fatalf("HeapAlloc %d exceeds heap limit %d", m2.HeapAlloc, limit)
		}
		ReadGCPolicy(&gcp)
		if gcp.MaxHeapBytes != limit {
			t.Fatalf("want MaxHeapBytes %d, got policy %+v", limit, gcp)
		}
		if gcp.HeapGoal > uint64(limit) || float64(gcp.HeapGoal) < float64(limit)*(availErr/100) {
			t.Fatalf("want HeapGoal %d, got policy %+v", limit, gcp)
		}
		// Avoid frequent ReadMemStats hindering GC mark.
		time.Sleep(time.Microsecond)
	}
	if m1.NumGC == m2.NumGC {
		t.Fatalf("failed to trigger GC")
	}
}

func TestGOGCAfterCloseMaxHeap(t *testing.T) {
	const gcPercent = 100
	defer debug.SetGCPercent(debug.SetGCPercent(gcPercent))
	defer func() {
		runtime.SetMaxHeap(math.MaxInt64)
		runtime.GC()
	}()

	runtime.GC()

	// Set max heap to 100MB.
	const limit = 100 << 20
	runtime.SetMaxHeap(limit)

	// Allocate a bunch and monitor HeapGoal.
	var gcp GCPolicy
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	ReadGCPolicy(&gcp)
	runtime.ReadMemStats(&m)
	if gcp.MaxHeapBytes != limit {
		t.Fatalf("want MaxHeapBytes %d, got %d", limit, gcp.MaxHeapBytes)
	}
	if m.NextGC > uint64(limit) || float64(m.NextGC) < float64(limit)*(availErr/100) {
		t.Fatalf("Wrong NextGC value %d, expect [%f~%f]", m.NextGC, float64(limit)*(availErr/100), float64(limit))
	}
	if gcp.GCPercent != gcPercent {
		t.Fatalf("want gcPercent %d, got %d", gcPercent, gcp.GCPercent)
	}
	runtime.SetMaxHeap(math.MaxInt64)
	runtime.GC()
	debug.SetGCPercent(200)
	runtime.GC()
	ReadGCPolicy(&gcp)
	if gcp.MaxHeapBytes != math.MaxInt64 {
		t.Fatalf("want MaxHeapBytes %d, got %d", ^uintptr(0), gcp.MaxHeapBytes)
	}
	if gcp.GCPercent != 200 {
		t.Fatalf("want gcPercent %d, got %d", 200, gcp.GCPercent)
	}
}

func TestMaxHeapBoundaryValue(t *testing.T) {
	const gcPercent = 100
	defer debug.SetGCPercent(debug.SetGCPercent(gcPercent))
	defer func() {
		runtime.SetMaxHeap(math.MaxInt64)
		runtime.GC()
	}()

	var gcp GCPolicy
	var m runtime.MemStats
	runtime.GC()

	// Set max heap to a larger value.
	var currMaxHeap int64 = math.MaxInt64 - 1
	runtime.SetMaxHeap(currMaxHeap)

	runtime.GC()
	ReadGCPolicy(&gcp)
	runtime.ReadMemStats(&m)
	if gcp.MaxHeapBytes != currMaxHeap {
		t.Fatalf("want MaxHeapBytes %d, got %d", currMaxHeap, gcp.MaxHeapBytes)
	}
	if m.NextGC > uint64(currMaxHeap) || float64(m.NextGC) < float64(currMaxHeap)*(availErr/100) {
		t.Fatalf("Wrong NextGC value %d, expect [%f~%f]", m.NextGC, float64(currMaxHeap)*(availErr/100), float64(currMaxHeap))
	}
	// Call SetMaxHeap with param -1, just get current maxHeap bytes.
	prev := runtime.SetMaxHeap(-1)
	if prev != currMaxHeap {
		t.Fatalf("want MaxHeapBytes %d, got %d", currMaxHeap, prev)
	}

	runtime.GC()
	ReadGCPolicy(&gcp)
	runtime.ReadMemStats(&m)
	if gcp.MaxHeapBytes != currMaxHeap {
		t.Fatalf("want MaxHeapBytes %d, got %d", currMaxHeap, gcp.MaxHeapBytes)
	}
	if m.NextGC > uint64(currMaxHeap) || float64(m.NextGC) < float64(currMaxHeap)*(availErr/100) {
		t.Fatalf("Wrong NextGC value %d, expect [%f~%f]", m.NextGC, float64(currMaxHeap)*(availErr/100), float64(currMaxHeap))
	}

	// Set max heap to 100MB.
	var prevMaxHeap = currMaxHeap
	currMaxHeap = 100 << 20
	prev = runtime.SetMaxHeap(currMaxHeap)
	if prev != prevMaxHeap {
		t.Fatalf("want MaxHeapBytes %d, got %d", prevMaxHeap, prev)
	}

	runtime.GC()
	ReadGCPolicy(&gcp)
	runtime.ReadMemStats(&m)
	if gcp.MaxHeapBytes != currMaxHeap {
		t.Fatalf("want MaxHeapBytes %d, got %d", currMaxHeap, gcp.MaxHeapBytes)
	}
	if m.NextGC > uint64(currMaxHeap) || float64(m.NextGC) < float64(currMaxHeap)*(availErr/100) {
		t.Fatalf("Wrong NextGC value %d, expect [%f~%f]", m.NextGC, float64(currMaxHeap)*(availErr/100), float64(currMaxHeap))
	}

	// Turn off the maxHeap.
	prevMaxHeap = currMaxHeap
	currMaxHeap = math.MaxInt64
	prev = runtime.SetMaxHeap(currMaxHeap)
	if prev != prevMaxHeap {
		t.Fatalf("want MaxHeapBytes %d, got %d", prevMaxHeap, prev)
	}

	runtime.GC()
	ReadGCPolicy(&gcp)
	runtime.ReadMemStats(&m)
	if gcp.MaxHeapBytes != currMaxHeap {
		t.Fatalf("want MaxHeapBytes %d, got %d", currMaxHeap, gcp.MaxHeapBytes)
	}
	// Turn off maxHeap, NextGC is decided by GOGC, it is not a fixed value.
}

// Enable SetMaxHeap by ENV MY_MEM_LIMIT and GODEBUG=gcHeapPercent.
func TestMaxHeapPercent1(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		defer func() {
			runtime.SetMaxHeap(math.MaxInt64)
			runtime.GC()
		}()

		runtime.GC()

		var limit int64 = 100 << 20

		var m runtime.MemStats
		var gcp GCPolicy
		sl := [][]byte{}
		for i := 0; i < 200; i++ {
			sl = append(sl, make([]byte, 1<<20))
			runtime.GC()
			runtime.ReadMemStats(&m)
			ReadGCPolicy(&gcp)
			// Use int64 to avoid overflow on 32-bit.
			avail := int(100 * (limit - int64(m.HeapAlloc)) / int64(m.HeapAlloc))
			if avail > 100 {
				avail = 100
			}
			if avail < 10 {
				avail = 10
			}
			if gcp.HeapGoal > uint64(float64(limit)*2*maxOvershoot) || float64(gcp.HeapGoal) < float64(limit)*(availErr/100) {
				t.Fatalf("want HeapGoal %d, got policy %+v", limit, gcp)
			}
		}
		return
	}
	var outb, errb bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestMaxHeapPercent1$")
	// Set max heap to 100MB.
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "MY_MEM_LIMIT="+strconv.Itoa(200<<20), "GODEBUG=gctrace=1,gcHeapPercent=50")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		t.Log(cmd.Stderr)
		t.Log(cmd.Stdout)
		t.Fatalf("Return error: %s\n", err)
	}
}

// Just set GODEBUG=gcHeapPercent, no MY_MEM_LIMIT ENV, SetMaxHeap does not take effect.
func TestMaxHeapPercent2(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		defer func() {
			runtime.SetMaxHeap(math.MaxInt64)
			runtime.GC()
		}()

		runtime.GC()

		const limit = 100 << 20

		var m runtime.MemStats
		var gcp GCPolicy
		sl := [][]byte{}
		outOfRange := false
		for i := 0; i < 200; i++ {
			sl = append(sl, make([]byte, 1<<20))
			runtime.GC()
			runtime.ReadMemStats(&m)
			ReadGCPolicy(&gcp)
			if gcp.HeapGoal > uint64(limit) || float64(gcp.HeapGoal) < float64(limit)*(availErr/100) {
				outOfRange = true
			}
		}
		if outOfRange == false {
			t.Fatalf("SetMaxHeap takes effect in unexpected scenarios")
		}
		return
	}
	var outb, errb bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestMaxHeapPercent2$")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "GODEBUG=gctrace=1,gcHeapPercent=50")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		t.Log(cmd.Stderr)
		t.Log(cmd.Stdout)
		t.Fatalf("Return error: %s\n", err)
	}
}

// Just set MY_MEM_LIMIT, ensure that the program is normal when maxheap is closed.
func TestMaxHeapPercent3(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		defer func() {
			runtime.SetMaxHeap(math.MaxInt64)
			runtime.GC()
		}()

		runtime.GC()

		const limit = 100 << 20

		var m runtime.MemStats
		var gcp GCPolicy
		sl := [][]byte{}
		outOfRange := false
		for i := 0; i < 200; i++ {
			sl = append(sl, make([]byte, 1<<20))
			runtime.GC()
			runtime.ReadMemStats(&m)
			ReadGCPolicy(&gcp)
			// Use int64 to avoid overflow on 32-bit.
			avail := int(100 * (limit - int64(m.HeapAlloc)) / int64(m.HeapAlloc))
			if avail > 100 {
				avail = 100
			}
			if avail < 10 {
				avail = 10
			}
			if gcp.HeapGoal > uint64(limit) || float64(gcp.HeapGoal) < float64(limit)*(availErr/100) {
				outOfRange = true
			}
		}
		if outOfRange == false {
			t.Fatalf("SetMaxHeap takes effect in unexpected scenarios")
		}
		return
	}
	var outb, errb bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestMaxHeapPercent3$")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "MY_MEM_LIMIT="+strconv.Itoa(200<<20), "GODEBUG=gctrace=1")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		t.Log(cmd.Stderr)
		t.Log(cmd.Stdout)
		t.Fatalf("Return error: %s\n", err)
	}
}

// Enable SetMaxHeap by ENV MY_MEM_LIMIT and GODEBUG=gcHeapPercent on GOGC=off.
func TestMaxHeapPercent4(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		defer func() {
			runtime.SetMaxHeap(math.MaxInt64)
			runtime.GC()
		}()

		runtime.GC()

		const limit = 400 << 20 * 0.7

		var m runtime.MemStats
		var gcp GCPolicy
		sl := [][]byte{}
		for i := 0; i < 200; i++ {
			sl = append(sl, make([]byte, 1<<20))
			runtime.GC()
			runtime.ReadMemStats(&m)
			ReadGCPolicy(&gcp)
			if gcp.MaxHeapBytes != limit {
				t.Fatalf("Wrong MaxHeapBytes value %d, expect %f", gcp.MaxHeapBytes, limit)
			}

			if m.NextGC > uint64(limit) || float64(m.NextGC) < float64(limit)*(availErr/100) {
				t.Fatalf("Wrong NextGC value %d, expect [%f~%f]", m.NextGC, float64(limit)*(availErr/100), float64(limit))
			}
		}
		return
	}
	var outb, errb bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestMaxHeapPercent4$")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "MY_MEM_LIMIT="+strconv.Itoa(400<<20), "GOGC=off", "GODEBUG=gctrace=1,gcHeapPercent=70")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		t.Log(cmd.Stderr)
		t.Log(cmd.Stdout)
		t.Fatalf("Return error: %s\n", err)
	}
}

//go:linkname gcSetMaxHeapFunc runtime.gcSetMaxHeapFunc
var gcSetMaxHeapFunc func(bytes int64) int64

//go:linkname gcSetMaxHeapFuncNotExist runtime.gcSetMaxHeapFuncNotExist
var gcSetMaxHeapFuncNotExist func(bytes int64) int64

func TestExportMaxHeap(t *testing.T) {
	if gcSetMaxHeapFuncNotExist != nil {
		t.Errorf("linkname a function that does not exist return not nil")
	}

	if gcSetMaxHeapFunc == nil {
		t.Errorf("linkname an existing function return nil")
	}

	if curr := gcSetMaxHeapFunc(-1); curr <= 0 {
		t.Errorf("call gcSetMaxHeap returned an incorrect value")
	}
}

func TestMaxHeapThresholdExceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}
	const (
		MemLimit       = 400 << 20
		MaxHeapPercent = 50
	)

	if os.Getenv("SUB_PROCESS") == "1" {
		defer func() {
			setGCPercentBallast = nil
			runtime.SetMaxHeap(math.MaxInt64)
			runtime.GC()
		}()

		const live = 400 << 20 * (MaxHeapPercent + 10) / 100
		setGCPercentBallast = make([]byte, live)

		go func() {
			for {
				setGCPercentSink = make([]byte, 100<<10)
				time.Sleep(time.Millisecond)
			}
		}()
		var m runtime.MemStats
		for i := 0; i < 5; i++ {
			time.Sleep(time.Second)
			runtime.ReadMemStats(&m)
			if m.NextGC < uint64(float64(live)*1.1) {
				t.Fatalf("Wrong NextGC value %d, expect >= %d", m.NextGC, uint64(float64(live)*1.1))
			}
			if m.GCCPUFraction > 0.03 {
				t.Fatalf("GC consumes too much CPU")
			}
		}
		return
	}
	// SetMaxHeap
	var outb, errb bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestMaxHeapThresholdExceeded$")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "MY_MEM_LIMIT="+strconv.Itoa(MemLimit), "GODEBUG=gctrace=1,gcHeapPercent="+strconv.Itoa(MaxHeapPercent))
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		t.Log(cmd.Stderr)
		t.Log(cmd.Stdout)
		t.Fatalf("Return error: %s\n", err)
	}

	// SetMemortLimit
	var outb2, errb2 bytes.Buffer
	cmd2 := exec.Command(os.Args[0], "-test.run=^TestMaxHeapThresholdExceeded$")
	cmd2.Env = append(os.Environ(), "SUB_PROCESS=1", "MY_MEM_LIMIT="+strconv.Itoa(MemLimit), "GOGC=off", "GODEBUG=gctrace=1", "GOMEMLIMIT="+strconv.Itoa(MemLimit*MaxHeapPercent/100))
	cmd2.Stdout = &outb2
	cmd2.Stderr = &errb2
	err2 := cmd2.Run()
	if err2 == nil {
		t.Log(cmd2.Stderr)
		t.Log(cmd2.Stdout)
		t.Fatalf("SetMemortLimit unexpect success")
	}
}
