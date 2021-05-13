// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"fmt"
	"internal/testenv"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestGabAllocBasic(t *testing.T) {
	output := runTestProg(t, "testproggab", "AllocBasic", "GODEBUG=gab=1")
	want := "OK\n"
	if output != want {
		t.Errorf("expected %q got %v", want, output)
	}
}

func TestGabObjFinalizer(t *testing.T) {
	output := runTestProg(t, "testproggab", "ObjFinalizer", "GODEBUG=gab=1")
	want := "OK\n"
	if output != want {
		t.Errorf("expected %q got %v", want, output)
	}
}

func testGabInChildProc(t *testing.T, name string) bool {
	if os.Getenv("GAB_TEST") != "" {
		return true
	}
	testenv.MustHaveGoBuild(t)
	cmd := exec.Command(testenv.GoToolPath(t), "test", fmt.Sprintf("-run=%s", name))
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GAB_TEST=true")

	if out, er := cmd.CombinedOutput(); er != nil {
		t.Fatalf("Failed to test %s:%s", name, out)
	}
	return false
}

// help objs to escape
var refs3 []unsafe.Pointer

func TestGabGrowing(t *testing.T) {
	if !testGabInChildProc(t, "GabGrowing") {
		return
	}

	refs3 = make([]unsafe.Pointer, 1)
	var failures int64
	var lastFailMsg string
	var wg sync.WaitGroup
	wg.Add(1)

	// called from another goroutine
	failed := func(msg string) {
		atomic.AddInt64(&failures, 1)
		lastFailMsg = msg
	}

	go func() {
		runtime.SetGAB(1)
		defer runtime.SetGAB(0)
		defer wg.Done()
		base, top, end := runtime.GetGABInfo()
		if base != 0 || top != 0 || end != 0 {
			failed("Should all be nil")
			return
		}

		const grain int = 125
		const gabInit uintptr = runtime.GabInitCap
		var gabMax uintptr = *runtime.GabMaxCap // current upper bound
		if gabMax > 32*1024 {
			failed("gabMaxCap cannot exceed maxSmallSize")
			return
		}

		if gabMax < gabInit {
			failed("gabMaxCap cannot less than gabInitCap")
			return
		}

		// first init
		refs3[0] = unsafe.Pointer(&[grain]byte{})
		base, top, end = runtime.GetGABInfo()
		if base == 0 || top < base || top >= end || end-base != gabInit {
			failed(fmt.Sprintf("Should be 4096KB after first initialization, but got [%d-%d-%d)\n", base, top, end))
			return
		}

		// keep allocating to trigger each level of growing
		nextThresh := gabInit
		var allocated uintptr
		for {
			refs3[0] = unsafe.Pointer(&[grain]byte{})
			allocated += uintptr(grain)
			if allocated > nextThresh {
				// this assert only supports current impl, in the future
				// if the upper bound is changed to != 2^n, pls update here
				nextThresh <<= 1
				base, top, end = runtime.GetGABInfo()
				nCap := end - base
				if nCap > gabMax || nCap%gabInit != 0 {
					failed(fmt.Sprintf("Cannot exceed gab limit, %d", nCap))
					return
				}
				if nCap == gabMax {
					break
				}
				if nCap != nextThresh {
					failed(fmt.Sprintf("Should have been doubled!, expect=%d, actual=%d\n", nextThresh, nCap))
					return
				}
				allocated = 0
			}
		}

		// when reach here, capacity should be gabMax,
		// then we check if gabMax is kept at next several growing
		for itr := 0; itr < 5; itr++ {
			for allocated = 0; allocated < (gabMax + uintptr(grain)); {
				refs3[0] = unsafe.Pointer(&[grain]byte{})
				allocated += uintptr(grain)
			}
			base, top, end = runtime.GetGABInfo()
			if end-base != gabMax {
				failed(fmt.Sprintf("Should always be gabMax, expected=%d, actual=%d\n", gabMax, end-base))
				return
			}
		}
	}()
	wg.Wait()

	if failures != 0 {
		if lastFailMsg != "" {
			t.Logf("%s\n", lastFailMsg)
		}
		t.Fatalf("Failures from sub-goroutines!")
	}
}

// help with escaping
var refs4 []unsafe.Pointer

func BenchmarkSmallGarbage(b *testing.B) {
	refs4 = make([]unsafe.Pointer, 1)
	// small sizes
	sizes := []int{1, 2, 4, 7, 8, 32, 45, 65, 77, 85, 99, 127}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % len(sizes)
		o := make([]byte, sizes[idx])
		refs4[0] = unsafe.Pointer(&o)
	}
	b.StopTimer()
}

func TestGabMaxCap(t *testing.T) {
	gabToCap := map[int]int{
		-1:    0,
		0:     0,
		1:     16384,
		2:     16384,
		1023:  16384,
		1024:  1024,
		1025:  1024,
		8191:  7168,
		8192:  8192,
		8193:  8192,
		12288: 12288,
		12289: 12288,
		16384: 16384,
		16385: 16384,
		20480: 20480,
		32768: 32768,
		32769: 16384}

	for key, val := range gabToCap {
		out := runTestProg(t, "testproggab", "EchoMaxCap", fmt.Sprintf("GODEBUG=gab=%d", key))
		uv, err := strconv.ParseUint(out, 10, 64)
		if err != nil {
			t.Fatalf("Failed to parse output for uint64: %s", out)
		}
		if uv != uint64(val) {
			t.Fatalf("Failed to set GODEBUG=gab=%d; actual found gab=%d\n", key, uv)
		}
	}
}

var refsUsedBytes []unsafe.Pointer

func TestGabUsedBytes(t *testing.T) {
	if !testGabInChildProc(t, "GabUsedBytes") {
		return
	}

	refsUsedBytes = make([]unsafe.Pointer, 1)
	var gabInitSlotSize uintptr = runtime.GabInitCap // TCmalloc slot size for initial GAB capacity
	var wg sync.WaitGroup

	runtime.SetGAB(2048) // ensure policy disabled, thus runtime.GC will not disturb this testcase
	defer runtime.SetGAB(0)

	// clear memory to prevent GC right after starting this test
	runtime.GC()
	prevBytes := *runtime.GabUsedBytes

	wg.Add(1)
	go func() {
		buf := make([]byte, 72)
		refsUsedBytes[0] = unsafe.Pointer(&buf)
		wg.Done()
	}()
	wg.Wait()

	curBytes := *runtime.GabUsedBytes
	delta := curBytes - prevBytes
	if curBytes <= prevBytes || delta != gabInitSlotSize {
		t.Fatalf("expecting %d bytes allocated in gab, but got %d", gabInitSlotSize, delta)
	}
	prevBytes = curBytes

	// clear live refs
	refsUsedBytes[0] = nil
	// trigger two GCs, the second GC will definitely sweep what has been
	// identified as garbage in the first GC, according to current GC impl.
	// so it can guarantee to reclaim the target GAB garbage memory.
	runtime.GC()
	runtime.GC()

	curBytes = *runtime.GabUsedBytes
	delta = prevBytes - curBytes
	if prevBytes <= curBytes || delta != gabInitSlotSize {
		t.Fatalf("expecting 4096 bytes sweeped for gab, but got %d", delta)
	}
}

// testing for maximum limit of GAB
func TestGabPolicyMemPercentLimit(t *testing.T) {
	output := runTestProg(t, "testproggab", "MemoryPercentLimit", "GODEBUG=gab=1")
	t.Logf("%s", output)
}

func TestGabDebugOpt(t *testing.T) {
	//valid percent values
	pctToTest := []int64{1, 10, 20, 50, 90, 99}
	for _, pct := range pctToTest {
		output := runTestProg(t, "testproggab", "EchoMemoryPercent", fmt.Sprintf("GOGABOPT=gabPercent=%d", pct), "GODEBUG=gab=1")
		iv, err := strconv.ParseInt(output, 10, 32)
		if err != nil {
			t.Fatalf("Failed to parse output for uint64: %s", output)
		}
		if iv != pct {
			t.Log(output)
			t.Fatalf("Trying to set GOGABOPT=gabPercent=%d, but get %d", pct, iv)
		}
	}
	//invalid percent values
	pctToTest = []int64{-1, 100, 1024}
	for _, pct := range pctToTest {
		// should fail
		output := runTestProg(t, "testproggab", "EchoMemoryPercent", fmt.Sprintf("GOGABOPT=gabPercent=%d", pct), "GODEBUG=gab=1")
		if !strings.Contains(output, "fatal error: Illegal gabMemoryPercent") {
			t.Log(output)
			t.Fatalf("Should fail with GOGABOPT=gabPercent=%d", pct)
		}
		// should pass even for illegal values if GAB off
		output = runTestProg(t, "testproggab", "EchoMemoryPercent", fmt.Sprintf("GOGABOPT=gabPercent=%d", pct), "GODEBUG=gab=0")
		if strings.Contains(output, "fatal error: Illegal gabMemoryPercent") {
			t.Log(output)
			t.Fatalf("Should fail with GOGABOPT=gabPercent=%d", pct)
		}
	}
}
