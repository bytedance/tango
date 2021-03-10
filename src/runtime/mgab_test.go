// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"fmt"
	"internal/testenv"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// to make references escape
var refs1 []unsafe.Pointer

func TestGabAllocBasic(t *testing.T) {
	refs1 = make([]unsafe.Pointer, 2)

	// turn on gab (assuming always started with GAB off)
	runtime.SetGAB(true)
	defer runtime.SetGAB(false)

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
		t.Fatalf("addr1 (0x%x) addr2 (0x%x) should be in same GAB\n", addr1, addr2)
	}
}

var refs2 []unsafe.Pointer

func TestGabObjFinalizer(t *testing.T) {
	refs2 = make([]unsafe.Pointer, 1)
	var cnt uintptr
	var wg sync.WaitGroup
	wg.Add(1)

	// do GC firstly to prevent an about-to-GC state
	runtime.GC()

	// turn on gab
	runtime.SetGAB(true)
	defer runtime.SetGAB(false)

	go func() {
		defer wg.Done()
		bytes := new([15]byte) // bytes is supposedd to escape and allocated in gab
		refs2[0] = unsafe.Pointer(bytes)
		runtime.SetFinalizer(bytes, func(v *[15]byte) {
			runtime.Xadduintptr(&cnt, 1)
		})
	}()
	wg.Wait()

	// since we have triggered GC manually at the beginning,
	// so it is unlikely to have another GC right here
	if cnt != 0 || refs2[0] == nil {
		t.Fatalf("incorrect state: cnt=%d, refs2[0]=%p\n", cnt, refs2[0])
	}
	refs2[0] = nil // clear the ref

	// trigger GC to collect the dead object and trigger finalizer
	runtime.GC()
	runtime.GC()
	// sleep for a while to wait for finalizer to finish
	time.Sleep(3 * time.Second)

	if cnt != 1 {
		t.Fatalf("cnt must have been incremented by finalizer")
	}
}

// help objs to escape
var refs3 []unsafe.Pointer

func TestGabGrowing(t *testing.T) {
	refs3 = make([]unsafe.Pointer, 1)
	var failed bool
	var failMsg string
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		runtime.SetGAB(true)
		defer runtime.SetGAB(false)
		defer wg.Done()
		base, top, end := runtime.GetGABInfo()
		if base != 0 || top != 0 || end != 0 {
			failed = true
			failMsg = "Should all be nil"
			return
		}

		const grain int = 125
		const gabInit uintptr = 4 * 1024
		var gabMax uintptr = runtime.GetGabCapMax() // current upper bound
		if gabMax > 32*1024 {
			failed = true
			failMsg = "gabMaxCap cannot exceed maxSmallSize"
			return
		}

		if gabMax < gabInit {
			failed = true
			failMsg = "gabMaxCap cannot less than gabInitCap"
			return
		}

		// first init
		refs3[0] = unsafe.Pointer(&[grain]byte{})
		base, top, end = runtime.GetGABInfo()
		if base == 0 || top < base || top >= end || end-base != gabInit {
			failed = true
			failMsg = fmt.Sprintf("Should be 4096KB after first initialization, but got [%d-%d-%d)\n", base, top, end)
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
					failed = true
					failMsg = fmt.Sprintf("Cannot exceed gab limit, %d", nCap)
					return
				}
				if nCap == gabMax {
					break
				}
				if nCap != nextThresh {
					failed = true
					failMsg = fmt.Sprintf("Should have been doubled!, expect=%d, actual=%d\n", nextThresh, nCap)
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
				failed = true
				failMsg = fmt.Sprintf("Should always be gabMax, expected=%d, actual=%d\n", gabMax, end-base)
				return
			}
		}
	}()
	wg.Wait()

	if failed {
		t.Fatalf("Should have passed!%s", failMsg)
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

// prepare source directory for testing
func prepareSource(dest string) (func(), error) {
	pwd, err := os.Getwd()

	content := `
package runtime_test

import (
	"flag"
	"runtime"
	"testing"
)

var cap1 = flag.Int64("cap", 0, "expected cap")

func TestSetGabToGetCap(t *testing.T) {
        np := runtime.GetGabCapMax()
        if uintptr(*cap1) != np {
                t.Fatalf("setting debug.gab expected =%d but get %d\n ", *cap1, np)
        }
}
`

	fullPath := filepath.Join(pwd, dest)
	err = ioutil.WriteFile(fullPath, []byte(content), 0644)
	cleanup := func() {
		os.RemoveAll(fullPath)
	}
	return cleanup, err
}

func TestGabMaxCap(t *testing.T) {

	// prepare source
	testenv.MustHaveGoBuild(t)

	//execute and get output
	fname := "temp_test.go"
	cleanup, err := prepareSource(fname)
	if cleanup != nil {
		defer cleanup()
	}

	if err != nil {
		t.Fatal(err)
	}

	gabToCap := map[int]int{-1: 0, 0: 16384, 1: 16384, 2: 16384, 4095: 16384, 4096: 4096, 8191: 4096, 8192: 8192, 8193: 8192, 12288: 12288, 12289: 12288, 16384: 16384, 16385: 16384, 20480: 20480, 32768: 32768, 32769: 16384}

	for key, val := range gabToCap {
		arg := fmt.Sprintf("-cap=%d", val)
		envv := fmt.Sprintf("GODEBUG=gab=%d", key)

		cmd := exec.Command(testenv.GoToolPath(t), "test", "-run=SetGabToGetCap", "-args", arg)
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, envv)
		if out, er := cmd.CombinedOutput(); er != nil {
			t.Fatalf("Failed to test GODEBUG=gab=%d\n%s\n", key, out)
		}
	}

}
