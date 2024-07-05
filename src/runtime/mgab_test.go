// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"fmt"
	"internal/goarch"
	"internal/race"
	"internal/testenv"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func getGABEnvs() [][]string {
	var envs [][]string
	if isEvacSupported() {
		envs = [][]string{
			{
				"GODEBUG=gab=1",
			},
			{
				"GODEBUG=gab=2",
			},
			{
				"GODEBUG=gab=2",
				"GOGABOPT=evacuation=1",
			},
		}
	} else {
		envs = [][]string{
			{
				"GODEBUG=gab=1",
			},
		}
	}
	return envs
}

func getGABAllocStrEnvs() [][]string {
	var envs [][]string
	if isEvacSupported() {
		envs = [][]string{
			{
				"GODEBUG=gab=1",
				"GOGABOPT=gabStrAlloc=1",
			},
			{
				"GODEBUG=gab=2",
				"GOGABOPT=gabStrAlloc=1",
			},
			{
				"GODEBUG=gab=2",
				"GOGABOPT=evacuation=1,gabStrAlloc=1",
			},
		}
	} else {
		envs = [][]string{
			{
				"GODEBUG=gab=1",
				"GOGABOPT=gabStrAlloc=1",
			},
		}
	}
	return envs
}

func getGABAllocStrHeaderEnvs() [][]string {
	var envs [][]string
	if isEvacSupported() {
		envs = [][]string{
			{
				"GODEBUG=gab=1",
				"GOGABOPT=gabStrAlloc=1,gabStrHeaderAlloc=1",
			},
			{
				"GODEBUG=gab=2",
				"GOGABOPT=gabStrAlloc=1,gabStrHeaderAlloc=1",
			},
			{
				"GODEBUG=gab=2",
				"GOGABOPT=evacuation=1,gabStrAlloc=1,gabStrHeaderAlloc=1",
			},
		}
	} else {
		envs = [][]string{
			{
				"GODEBUG=gab=1",
				"GOGABOPT=gabStrAlloc=1,gabStrHeaderAlloc=1",
			},
		}
	}
	return envs
}

func isEvacSupported() bool {
	return (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && (runtime.GOOS == "linux" || runtime.GOOS == "darwin")
}

func TestGabAllocBasic(t *testing.T) {
	for _, env := range getGABEnvs() {
		output := runTestProg(t, "testproggab", "AllocBasic", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabAllocStr(t *testing.T) {
	for _, env := range getGABAllocStrEnvs() {
		output := runTestProg(t, "testproggab", "AllocStr", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabAllocStrHeader(t *testing.T) {
	for _, env := range getGABAllocStrHeaderEnvs() {
		output := runTestProg(t, "testproggab", "AllocStrHeader", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabObjFinalizer(t *testing.T) {
	for _, env := range getGABEnvs() {
		output := runTestProg(t, "testproggab", "ObjFinalizer", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabReuseBuffer(t *testing.T) {
	for _, env := range getGABAllocStrHeaderEnvs() {
		output := runTestProg(t, "testproggab", "ReuseBuffer", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabAllocZeroSizeString(t *testing.T) {
	for _, env := range getGABAllocStrHeaderEnvs() {
		output := runTestProg(t, "testproggab", "AllocZeroSizeString", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabMarkStrBody(t *testing.T) {
	for _, env := range getGABAllocStrHeaderEnvs() {
		output := runTestProg(t, "testproggab", "MarkStrBody", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabMarkStrBodyWithAllocHeader(t *testing.T) {
	for _, env := range getGABAllocStrHeaderEnvs() {
		output := runTestProg(t, "testproggab", "MarkStrBodyWithAllocHeader", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
	}
}

func TestGabMarkStrBodyWriteBarrier(t *testing.T) {
	output := runTestProg(t, "testproggab", "MarkStrBodyWriteBarrier",
		"GODEBUG=gab=1", "GOGABOPT=gabStrHeaderAlloc=1")
	want := "OK\n"
	if output != want {
		t.Errorf("expected %q got %v", want, output)
	}
}

func TestGabMarkStrBodyConservative(t *testing.T) {
	for _, env := range getGABAllocStrHeaderEnvs() {
		output := runTestProg(t, "testproggab", "MarkStrBodyConservative", env...)
		want := "OK\n"
		if output != want {
			t.Errorf("expected %q got %v", want, output)
		}
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
	if race.Enabled {
		t.Skip("-race")
	}
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
	maxHeap := 1 << 30
	envs := [][]string{
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
			"GODEBUG=gab=1,gcHeapPercent=50",
		},
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
			"GODEBUG=gab=2,gcHeapPercent=50",
		},
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
			"GODEBUG=gab=2,gcHeapPercent=50",
			"GOGABOPT=evacuation=1",
		},
	}
	want := "OK\n"
	for _, env := range envs {
		output := runTestProg(t, "testproggab", "MemoryPercentLimit", env...)
		if !strings.HasSuffix(output, want) {
			t.Errorf("%s", output)
		}
	}
}

// testing the hard memory limit of GAB memory.
// Use the environment variable MY_MEM_LIMIT to specify the memory limit.
func TestGabHardMemoryLimit(t *testing.T) {
	envs := [][]string{
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", 1<<28),
			"GODEBUG=gab=1",
		},
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", 1<<28),
			"GODEBUG=gab=2",
		},
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", 1<<28),
			"GODEBUG=gab=2",
			"GOGABOPT=evacuation=1",
		},
	}
	want := "OK\n"
	for _, env := range envs {
		output := runTestProg(t, "testproggab", "GabHardMemPctLimit", env...)
		if !strings.HasSuffix(output, want) {
			t.Errorf("%s", output)
		}
	}
}

// testing for GAB threshold check when refilling GAB buffer
func TestGabPausedThreshold(t *testing.T) {
	maxHeap := 1 << 30
	envs := [][]string{
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
			"GODEBUG=gab=1,gcHeapPercent=50",
			"GOGABOPT=gabStrAlloc=1",
		},
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
			"GODEBUG=gab=2,gcHeapPercent=50",
			"GOGABOPT=gabStrAlloc=1",
		},
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
			"GODEBUG=gab=2,gcHeapPercent=50",
			"GOGABOPT=evacuation=1,gabStrAlloc=1",
		},
	}

	want := "OK\n"
	for _, env := range envs {
		output := runTestProg(t, "testproggab", "GabPausedThreshold", env...)
		if !strings.HasSuffix(output, want) {
			t.Errorf("%s", output)
		}
	}
}

func TestGabDebugOpt(t *testing.T) {
	if race.Enabled {
		t.Skip("-race")
	}
	//valid percent values
	pctToTest := []int64{1, 10, 20, 50, 90, 99}
	for _, pct := range pctToTest {
		for _, env := range getGABEnvs() {
			output := runTestProg(t, "testproggab", "EchoMemoryPercent", append(env, fmt.Sprintf("GOGABOPT=gabPercent=%d", pct))...)
			iv, err := strconv.ParseInt(output, 10, 32)
			if err != nil {
				t.Fatalf("Failed to parse output for uint64: %s", output)
			}
			if iv != pct {
				t.Log(output)
				t.Fatalf("Trying to set GOGABOPT=gabPercent=%d, but get %d", pct, iv)
			}
		}
	}
	//invalid percent values
	pctToTest = []int64{-1, 100, 1024}
	for _, pct := range pctToTest {
		// should fail
		for _, env := range getGABEnvs() {
			output := runTestProg(t, "testproggab", "EchoMemoryPercent", append(env, fmt.Sprintf("GOGABOPT=gabPercent=%d", pct))...)
			if !strings.Contains(output, "fatal error: Illegal gabMemoryPercent") {
				t.Log(output)
				t.Fatalf("Should fail with GOGABOPT=gabPercent=%d", pct)
			}
		}
		// should pass even for illegal values if GAB off
		output := runTestProg(t, "testproggab", "EchoMemoryPercent", fmt.Sprintf("GOGABOPT=gabPercent=%d", pct))
		if strings.Contains(output, "fatal error: Illegal gabMemoryPercent") {
			t.Log(output)
			t.Fatalf("Should fail with GOGABOPT=gabPercent=%d", pct)
		}
	}
}

func TestGabInlineEmptyArray(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	source := `package main

type encodedKeyValues []encodedKV
type encodedKV struct {
	key      string
	keyValue []byte
}

var done chan bool
var g *encodedKeyValues

func alloc() {
	g = &encodedKeyValues{}
	done<-true
}

func main() {
	done = make(chan bool)
	go alloc()
	<-done
}
`
	testenv.MustHaveGoBuild(t)

	dir := t.TempDir()
	tf := filepath.Join(dir, "test.go")
	if err := ioutil.WriteFile(tf, []byte(source), 0666); err != nil {
		t.Fatalf("Failed to create %s: %v\n", tf, err)
	}
	cmd := exec.Command(testenv.GoToolPath(t), "run", "-opt=beast", "test.go")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GODEBUG=gab=1")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to verify gab inlining\n%s", out)
	}
}

func TestGabObjectSize(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	if !testGabInChildProcEnvs(t, "GabObjectSize", []string{"GODEBUG=gab=2"}) {
		return
	}

	m := make(map[unsafe.Pointer]struct{})
	for i := 0; i < 1024; i++ {
		p := allocGABObj()
		m[p] = struct{}{}
	}

	for k, _ := range m {
		p := uintptr(k)
		expected := unsafe.Sizeof(GABObject{})
		if actual, success := runtime.GetGABObjectSize(p); success {
			if expected != actual {
				t.Fatalf("Inconsistent GAB object size. expected %v, actual %v.", expected, actual)
			}
		} else {
			t.Fatalf("Fail to obtain object size.")
		}
	}
}

func TestGabVerification(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	if !testGabInChildProcEnvs(t, "GabVerification", []string{"GODEBUG=gab=2"}) {
		return
	}
	const Length = 1000
	var ptrs [Length]unsafe.Pointer
	// Allocate in GABs with vary-size objects so that we can see filler objects here.
	for i := 0; i < len(ptrs); i++ {
		if i%2 == 0 {
			ptrs[i] = allocGABObjArray()
		} else {
			ptrs[i] = allocGABObj()
		}
	}

	if !runtime.VerifyGABs() {
		t.Fatalf("Fail to find filler object in the GAB.")
	}
}

type GABObject struct {
	val int
}

//go:noinline
func allocGABObj() unsafe.Pointer {
	return unsafe.Pointer(&GABObject{})
}

//go:noinline
func allocGABObjArray() unsafe.Pointer {
	return unsafe.Pointer(&[8]GABObject{})
}

func testGabInChildProcEnvs(t *testing.T, name string, envs []string) bool {
	if os.Getenv("GAB_TEST") != "" {
		return true
	}
	testenv.MustHaveGoBuild(t)
	cmd := exec.Command(testenv.GoToolPath(t), "test", fmt.Sprintf("-run=%s", name))
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GAB_TEST=true")
	cmd.Env = append(cmd.Env, envs...)

	if out, er := cmd.CombinedOutput(); er != nil {
		t.Fatalf("Failed to test %s:%s", name, out)
	} else {
		t.Logf("Outout: %s", string(out))
	}
	return false
}

// We rely on objStartBits rather than object metaword
// to calculate the size of GAB-allocated objects.
// Since no object allocated on the RHS of the top object,
// using objStartBits to calculate its size may be invalid.
// In order to verify, we first stop the world and make GABs
// parsable, then perform verification.
func TestGabObjStartBits(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	if !testGabInChildProcEnvs(t, "GabObjStartBits", []string{"GODEBUG=gab=2"}) {
		return
	}

	const Length = 1000
	var ptrs [Length]unsafe.Pointer
	objSize := unsafe.Sizeof(GABObject{})

	for i := 0; i < len(ptrs); i++ {
		ptrs[i] = allocGABObj()
	}

	for i := 0; i < len(ptrs); i++ {
		if !runtime.VerifyObjStartBits(ptrs[:], objSize) {
			t.Fatalf("Fail to locate object start.")
		}
	}
}

//go:noinline
func allocFrozenMap() *map[*GABObject]struct{} {
	m := map[*GABObject]struct{}{}
	obj := &GABObject{}
	m[obj] = struct{}{}
	return &m
}

//go:noinline
func allocBigFrozenMap() *map[*GABObject]struct{} {
	m := make(map[*GABObject]struct{}, 1000)
	for i := range 1000 {
		obj := &GABObject{i}
		m[obj] = struct{}{}
	}
	return &m
}

func testGabInChildProcEnv(t *testing.T, name string, env ...string) bool {
	if os.Getenv("GAB_TEST") != "" {
		return true
	}
	testenv.MustHaveGoBuild(t)
	cmd := exec.Command(testenv.GoToolPath(t), "test", fmt.Sprintf("-run=%s", name))
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GAB_TEST=true")
	cmd.Env = append(cmd.Env, env...)

	if out, er := cmd.CombinedOutput(); er != nil {
		t.Fatalf("Failed to test %s:%s", name, out)
	}
	return false
}

// Verify the size of objStartBits.
// The size of GAB can be 1 KB, 2 KB, 4 KB, 8 KB, 16 KB and 32 KB.
// Get the sizeclass of GAB mspan according to the GAB size.
// Calculate the size of forzenBits according to the size of mspan.
func TestGabObjStartBitsSize(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	verifyObjStartBitsSize(t)
}

func verifyObjStartBitsSize(t *testing.T) {
	var gabSizes [6]uintptr
	var objStartBitsSizes = [6]uintptr{runtime.SmallObjStartBitsAllocSize, runtime.SmallObjStartBitsAllocSize,
		runtime.SmallObjStartBitsAllocSize, runtime.SmallObjStartBitsAllocSize, runtime.SmallObjStartBitsAllocSize,
		runtime.LargeObjStartBitsAllocSize}
	for i := 0; i < len(gabSizes); i++ {
		gabSizes[i] = uintptr(runtime.GABCapInitial << i)
	}

	for i := 0; i < len(gabSizes); i++ {
		gabSize := gabSizes[i]
		sizeclass := size2SizeClass(gabSize)
		page := int(runtime.Class_to_allocnpages[sizeclass])
		mspanSize := page << runtime.PageShift
		// One bit for a pointer-width memory block.
		nelems := mspanSize / goarch.PtrSize
		// Round up to 8 bytes.
		bytesNeeded := (nelems + 63) / 64 * 8

		if bytesNeeded > int(objStartBitsSizes[i]) {
			t.Fatalf("objStartBits overflows. mspan size %d B, objStartBits size %d B",
				mspanSize, bytesNeeded)
		}
	}
}

// Copy from mallocgc1()
func size2SizeClass(size uintptr) int8 {
	divRoundUp := func(n, a uintptr) uintptr {
		// a is generally a power of two. This will get inlined and
		// the compiler will optimize the division.
		return (n + a - 1) / a
	}

	var sc uint8
	if size <= uintptr(runtime.SmallSizeMax-8) {
		sc = runtime.Size_to_class8[divRoundUp(size, uintptr(runtime.SmallSizeDiv))]
	} else {
		sc = runtime.Size_to_class128[divRoundUp(size-uintptr(runtime.SmallSizeMax), uintptr(runtime.LargeSizeDiv))]
	}
	return int8(sc)
}

// Testing the live GAB objects in bytes collected during GC.
func TestGabMarked(t *testing.T) {
	// GOMAXPROCS=1 makes sure that only one GC worker.
	if !testGabInChildProcEnv(t, "GabMarked", "GODEBUG=gab=1", "GOMAXPROCS=1") {
		return
	}

	// to make gab decision to take effects
	ballast := make([]byte, 1<<25)
	runtime.GC()

	var gabAllocLimit = 1 << 25
	allocGrain := 128
	var allocBytes int
	var refs []unsafe.Pointer

	var wg sync.WaitGroup
	wg.Add(1)
	// allocation worker
	go func() {
		defer wg.Done()
		for allocBytes < gabAllocLimit {
			buf := make([]byte, allocGrain)
			ptr := unsafe.Pointer(&buf)
			refs = append(refs, ptr)
			allocBytes += allocGrain
		}
	}()
	wg.Wait()
	runtime.GC()
	beforeMarked := runtime.GabMarked()
	verifyGabMemory(t)

	// Clear half of the buffers
	for i := len(refs) / 2; i < len(refs); i++ {
		refs[i] = nil
	}
	runtime.GC()
	afterMarked := runtime.GabMarked()
	verifyGabMemory(t)

	if afterMarked >= beforeMarked {
		t.Fatalf("GAB Before marked %d B, after marked %d B", beforeMarked, afterMarked)
	} else {
		fmt.Printf("GAB Before marked %d B, after marked %d B\n", beforeMarked, afterMarked)
	}

	ballast[0] = 1
	refs = make([]unsafe.Pointer, 1)
}

func verifyGabMemory(t *testing.T) {
	// Make sure there are no more GAB allocations.
	*runtime.GabEnabled = false
	used := *runtime.GabUsedBytes
	marked := uintptr(runtime.GabMarked())
	live := runtime.LiveGabSize()

	// GABUsedBytes is GAB memory in bytes. It is updated during allocation and sweeping.
	// liveGABs is obtained by iterating over all GAB mspans.
	// gabMarked is collected during GC marking.
	// These three statistics are expected to be equal.
	// Because GC marking is performed concurrently, gabMarked could be
	// slightly larger than GABUsedBytes and liveGABs. Using only one
	// GC worker (specified by GOMAXPROCS) to avoid this issue.
	if used != live || marked != live {
		t.Fatalf("GAB uses %d B, marked %d B, live %d B", used, marked, live)
	}
	fmt.Printf("GAB uses %d B, marked %d B, live %d B\n", used, marked, live)
	*runtime.GabEnabled = true
}

// Check whether the size class of minFrozenMapBucketSize equals to
// the size of actual map bucket.
func TestGabMinFrozenMapBucketSize(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	m := allocFrozenMap()
	groupPtrs := runtime.GetSwissMapGroupPtrs(unsafe.Pointer(m))

	if len(groupPtrs) != 1 {
		t.Fatalf("Minimal map bucket has %d groups, expected 1", len(groupPtrs))
	}

	sc := size2SizeClass(runtime.MinFrozenMapBucketSize)
	groundTruth := runtime.SizeClass(uintptr(groupPtrs[0]))
	if sc != groundTruth {
		t.Fatalf("Minimal map bucket with frozenBits size %d, sizeclass %d, expected %d",
			runtime.MinFrozenMapBucketSize, sc, groundTruth)
	}
}

// Allocate maps with pointer pointing to GAB-allocated object.
// Then verify whether the map bucket is frozen.
// Test the small map in swissmap.
func TestGabFrozenBits(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	if !testGabInChildProcEnv(t, "GabVerifyFrozenBits", "GODEBUG=gab=2", "GOGABOPT=evacuation=1") {
		return
	}
	const length = 10000
	var ptrs [length]unsafe.Pointer
	for i := 0; i < length; i++ {
		ptrs[i] = unsafe.Pointer(allocFrozenMap())
	}

	go func() {
		for {
			runtime.GC()
			time.Sleep(time.Millisecond)
		}
	}()

	for i := 0; i < length; i++ {
		bucketPtr := map2GroupPtr(ptrs[i])
		if !runtime.IsFrozenObj(uintptr(bucketPtr)) {
			t.Fatalf("Object %x is expected to be frozen", bucketPtr)
		}
	}
}

// Allocate maps with pointer pointing to GAB-allocated object.
// Then verify whether the map bucket is frozen.
// Test the non small map in swissmap.
func TestGabFrozenBitsForBigMap(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	if !testGabInChildProcEnv(t, "TestGabFrozenBitsForBigMap", "GODEBUG=gab=2", "GOGABOPT=evacuation=1") {
		return
	}
	const length = 10000
	var ptrs [length]unsafe.Pointer
	for i := 0; i < length; i++ {
		ptrs[i] = unsafe.Pointer(allocBigFrozenMap())
	}

	go func() {
		for {
			runtime.GC()
			time.Sleep(time.Millisecond)
		}
	}()

	for i := 0; i < length; i++ {
		bucketPtrs := runtime.GetSwissMapGroupPtrs(ptrs[i])
		for _, ptr := range bucketPtrs {
			if ptr == nil {
				continue
			}
			if !runtime.IsFrozenObj(uintptr(ptr)) {
				t.Fatalf("Object %x is expected to be frozen", ptr)
			}
		}

	}
}

// Verify the size of frozenBits.
// Allocate a minimal size of map that can have frozenBits.
// From the size of that map bucket, verify each mspan.
// frozenBits should be less than 16 bytes.
func TestGabFrozenBitsSize(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	verifyFrozenBitsSize(t)
}

func verifyFrozenBitsSize(t *testing.T) {
	// Allocate a map. Normal map variable is a pointer
	// refers to an instance of hmap.
	// ptr is a pointer to map, so it refers to a pointer of hmap.
	ptr := unsafe.Pointer(allocFrozenMap())
	buckets := map2GroupPtr(ptr)
	minSizeClass := runtime.SizeClass(uintptr(buckets))
	minFrozenMapBktSizeClass := size2SizeClass(runtime.MinFrozenMapBucketSize)
	if minSizeClass < minFrozenMapBktSizeClass {
		t.Fatalf("Map bucket size class %d, expected %d", minSizeClass, minFrozenMapBktSizeClass)
	}
	for i := int(minSizeClass); i < runtime.NumSizeClasses; i++ {
		elemSize := int(runtime.Class_to_size[i])
		page := int(runtime.Class_to_allocnpages[i])
		mspanSize := page << runtime.PageShift
		// One bit for one object in mspan.
		nelems := mspanSize / elemSize
		// Round up to 8 bytes.
		bytesNeeded := ((nelems + 63) / 64) * 8

		if bytesNeeded > int(runtime.GABFrozenBitsAllocSize) {
			t.Fatalf("frozenBits overflows. mspan size %d B, element size %d B, frozenBits size %d B, actual size %d",
				mspanSize, elemSize, bytesNeeded, runtime.GABFrozenBitsAllocSize)
		}
	}
}

func map2GroupPtr(ptr unsafe.Pointer) unsafe.Pointer {
	return runtime.GetSwissMapGroupPtrs(ptr)[0]
}
