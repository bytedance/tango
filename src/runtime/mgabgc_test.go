// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"fmt"
	"internal/abi"
	"internal/race"
	"internal/testenv"
	"log"
	"maps"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func TestGabPinObj(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	output := runTestProg(t, "testproggab", "PinObjs", "GODEBUG=gab=2")
	t.Logf("%s", output)
	if strings.Contains(output, "panic") {
		t.Fatalf("Child testing process failed")
	}
}

func TestGabGCALot(t *testing.T) {
	for _, env := range getGABEnvs() {
		output := runTestProg(t, "testproggab", "GCALot", env...)
		t.Logf("%s", output)
		if strings.Contains(output, "panic") {
			t.Fatalf("Child testing process failed")
		}
	}

}

func TestGabMemSim(t *testing.T) {
	if race.Enabled {
		t.Skip("-race")
	}
	var envs = [][]string{
		{
			"GODEBUG=gab=1,gctrace=1",
			"GAB_TEST_ENV=-d=5s",
		},
		{
			"GODEBUG=gab=2,gctrace=1",
			"GAB_TEST_ENV=-d=5s",
		},
		{
			"GODEBUG=gab=2,gctrace=1",
			"GOGABOPT=evacuation=1",
			"GAB_TEST_ENV=-d=10s",
		},
		{
			"GODEBUG=gab=2,gctrace=1",
			"GOGABOPT=evacuation=1",
			"GAB_TEST_ENV=-d=10s",
		},
	}

	for _, env := range envs {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "MemSim", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

// Test whether GAB evacuation properly handle specials.
func TestGabSpecialRelocation(t *testing.T) {
	for _, env := range getGABEnvs() {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "SpecialRelocation", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

// Recursively install a finalizer on an object,
// which cannot be reclaimed in this case.
func TestGabSpecialRecursiveFinalizers(t *testing.T) {
	for _, env := range getGABEnvs() {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "InstallFinalizerRecursion", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

func TestGabMapWithGABPointerAsKey(t *testing.T) {
	for _, env := range getGABEnvs() {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "MapWithGABPointerAsKey", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

func TestGabMapWithNormalObjectAsKey(t *testing.T) {
	for _, env := range getGABEnvs() {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "MapWithNormalObjectAsKey", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

func TestGabMapAsyncPreemption(t *testing.T) {
	for _, env := range getGABEnvs() {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "GabMapAsyncPreemption", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

// Before stack scanning is enabled, enable balanced GC
// during test case compilation has the potential to
// trigger the crash.
func TestGabStackRescanning(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	testenv.MustHaveGoBuild(t)
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	const FREQ = 5

	// Use separate GOPATH and GOCACHE to prevent this case from
	// interfering others at 'dist test'.
	gopath := t.TempDir()
	gocache := t.TempDir()
	newCmd := func(name string, arg ...string) *exec.Cmd {
		cmd := exec.Command(name, arg...)
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "GOPATH="+gopath)
		cmd.Env = append(cmd.Env, "GOCACHE="+gocache)
		return cmd
	}

	for i := 0; i < FREQ; i++ {
		dir := t.TempDir()
		t.Logf("Working directory: %s\n", dir)

		newCmd(testenv.GoToolPath(t), "clean").CombinedOutput()
		newCmd(testenv.GoToolPath(t), "clean", "-cache").CombinedOutput()
		newCmd(testenv.GoToolPath(t), "clean", "-testcache").CombinedOutput()

		cmd := newCmd(testenv.GoToolPath(t), "test", "-v", "-c", "-o", "test", "-run=Gab", "runtime")
		// Enable balanced GC
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "GODEBUG=gab=2")
		cmd.Env = append(cmd.Env, "GOGABOPT=evacuation=1")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Error occurred.\n%s", string(out))
		}
	}
}

// Concurrently performing GAB GC bits cleaning.
func TestGabConcGCBitsClean(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	var envs = [][]string{
		{
			"GODEBUG=gab=2",
		},
		{
			"GODEBUG=gab=2",
			"GOGABOPT=evacuation=1",
		},
	}

	for _, env := range envs {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "ConcGCBitsClean", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

func TestGabFindBlockFast(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	if race.Enabled {
		t.Skip("-race")
	}
	maxHeap := 4 << 30
	var envs = [][]string{
		{
			fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
			"GODEBUG=gab=2,gcHeapPercent=50",
			"GOGABOPT=evacuation=1,trace=0",
		},
	}

	for _, env := range envs {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "FindBlockFast", env...)
			t2.Logf("%s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

func TestGabMarkStacks(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	if race.Enabled {
		t.Skip("-race")
	}
	var envs = [][]string{
		{
			"GODEBUG=gab=2,gctrace=0",
			"GOGABOPT=evacuation=1,trace=0",
		},
	}

	for _, env := range envs {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "GabMarkStacks", env...)
			t2.Logf("Output: %s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

// Allocate large enough objects to trigger gcProg case and
// check whether balanced GC stack rescanning can properly
// handle gcProg.
func TestGabRescanGCProg(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	var envs = [][]string{
		{
			"GODEBUG=gab=2,gctrace=0",
			"GOGABOPT=evacuation=1,trace=0",
		},
	}

	for _, env := range envs {
		t.Run(strings.Join(env, " "), func(t2 *testing.T) {
			output := runTestProg(t2, "testproggab", "GabRescanGCProg", env...)
			t2.Logf("Output: %s", output)
			if strings.Contains(output, "panic") {
				t2.Fatalf("Child testing process failed")
			}
		})
	}
}

func TestGabNumEvac(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	// Use max heap to avoid any non-forced GC
	maxHeap := 5 << 30
	var evacInterval uint32 = 5
	var envs = []string{
		fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
		"GODEBUG=gab=2,gcHeapPercent=50,gctrace=1",
		fmt.Sprintf("GOGABOPT=evacuation=%d,trace=0", evacInterval),
	}

	if !testGabInChildProcEnv(t, "GabNumEvac", envs...) {
		return
	}

	var allocFreq uint32 = 10
	var gcFreq uint32 = 10
	numEvac := gcFreq / evacInterval

	var wg sync.WaitGroup

	// Allocate GAB objects
	wg.Add(1)
	gabHolder := make([]unsafe.Pointer, 1<<20)
	nonGabHolder := make([]unsafe.Pointer, 1<<20)
	go func() {
		defer wg.Done()
		for i := uint32(0); i < allocFreq; i++ {
			for j := 0; j < len(gabHolder); j++ {
				b1 := make([]byte, 8)
				gabHolder[j] = unsafe.Pointer(&b1)
				b2 := make([]*int, 16)
				nonGabHolder[j] = unsafe.Pointer(&b2)
			}
		}
	}()

	// Triggering GC
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint32(0); i < gcFreq; i++ {
			runtime.GC()
			time.Sleep(100 * time.Millisecond)
		}
		println("Finish force GC")
	}()

	wg.Wait()
	// Evaluate memstats
	var memstats runtime.MemStats
	runtime.ReadMemStats(&memstats)
	actualGC := memstats.NumGC
	actualEvac := memstats.NumBalancedGC

	if gcFreq != actualGC {
		log.Fatalf("Incorrect numgc. Expected %d, actual %d", gcFreq, actualGC)
	}

	if numEvac != actualEvac {
		log.Fatalf("Incorrect numEvac. Expected %d, actual %d", numEvac, actualEvac)
	}
}

// Test switching from balanced GC to GAB.
func TestGabBalancedGC2GAB(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	maxHeap := 4 << 30

	var envs = []string{
		fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
		"GODEBUG=gab=2,gcHeapPercent=50,gctrace=1",
		"GOGABOPT=evacuation=1,trace=1",
	}

	if !testGabInChildProcEnv(t, "GabBalancedGC2GAB", envs...) {
		return
	}

	// Allocate GAB objects with balanced GC allocator
	gab2Holder := make([]unsafe.Pointer, 5)
	rand.Seed(time.Now().UnixNano())
	min := 8
	max := 128
	getSize := func() int {
		return min + rand.Intn(max-min+1)
	}

	{
		var wg1 sync.WaitGroup
		for i := 0; i < 5; i++ {
			i := i
			wg1.Add(1)
			ptrs := make([]unsafe.Pointer, 1<<12)
			go func() {
				for j := 0; j < 1<<12; j++ {
					buf := make([]byte, getSize())
					ptrs[j] = unsafe.Pointer(&buf)
				}
				gab2Holder[i] = unsafe.Pointer(&ptrs)
				wg1.Done()
			}()
		}
		wg1.Wait()
	}
	var memstat runtime.MemStats
	runtime.ReadMemStats(&memstat)
	println(gab2Holder)

	// Switch to GAB allocator
	runtime.BalancedGC2GAB()
	gabSysBefore := runtime.GetGabSysMemUsage()

	gab1Holder := make([]unsafe.Pointer, 5)
	// Allocate GAB objects with GAB allocator
	{
		var wg2 sync.WaitGroup
		for i := 0; i < 5; i++ {
			i := i
			wg2.Add(1)
			ptrs := make([]unsafe.Pointer, 1<<12)
			go func() {
				for j := 0; j < 1<<12; j++ {
					buf := make([]byte, getSize())
					ptrs[j] = unsafe.Pointer(&buf)
				}
				gab1Holder[i] = unsafe.Pointer(&ptrs)
				wg2.Done()
			}()
		}
		wg2.Wait()
	}
	runtime.ReadMemStats(&memstat)
	println(gab1Holder)
	println(gab2Holder)

	// Perform GC
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
	}
	runtime.ReadMemStats(&memstat)

	// Release memory
	gab1Holder = make([]unsafe.Pointer, 5)
	gab2Holder = make([]unsafe.Pointer, 5)

	// Perform GC
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
	}
	runtime.ReadMemStats(&memstat)
	gabSysAfter := runtime.GetGabSysMemUsage()

	// Check gab_sys
	if gabSysBefore != gabSysAfter {
		t.Fatalf("gab_sys should not increased when GAB allocator is used, before %d after %d", gabSysBefore, gabSysAfter)
	}
}

// Allocate some GAB and non-GAB objects and let GAB pacer to
// estimate the ratio of GAB objects. If the estimated ratio
// is larger than a threshold, an error is raised.
func TestGabPacer(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	if race.Enabled {
		t.Skip("-race")
	}
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	maxHeap := 4 << 30
	var envs = []string{
		fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
		"GODEBUG=gab=2,gcHeapPercent=50,gctrace=1",
		"GOGABOPT=gabPacer=1,trace=1",
	}

	if !testGabInChildProcEnv(t, "GabPacer", envs...) {
		return
	}

	length := 1 << 20
	gabHolder := make([]unsafe.Pointer, length)
	nonGabHolder := make([]unsafe.Pointer, length)

	for gi := 0; gi < 5; gi++ {
		go func() {
			for i := 0; ; i++ {
				// Allocate scan objects
				x := make([]unsafe.Pointer, 16)
				nonGabHolder[i%length] = unsafe.Pointer(&x)
				// Allocate GAB objects
				y := make([]byte, 32)
				gabHolder[i%length] = unsafe.Pointer(&y)
			}
		}()
	}

	time.Sleep(15 * time.Second)

	println(&gabHolder)
	println(&nonGabHolder)

	error := math.Abs(runtime.GetGabPacerError())
	epsilon := 0.01
	if error > epsilon {
		log.Fatalf("GAB pacer error is too large %f, threshold %f\n", error, epsilon)
	} else {
		log.Printf("GAB pacer error %f, threshold %f", error, epsilon)
	}
}

// If the size of map key exceeds 128 B, the pointer of the key object
// is stored in the map buckets, rather than the large object itself.
// Coincidentally, the threshold of GAB objects is also 128 B. Therefore,
// indirect keys are not allocated in GABs. Consequently, those map keys
// do not need to be pinned, since they are not allocated in GABs and
// are not copied by balanced GC.
// If either the threshold of GAB objects or map indirect key is changed,
// indirect keys should be reconsidered to be pinned. This test case
// checks the threshold of both GAB objects and indirect keys.
func TestGabThreshold(t *testing.T) {
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}

	gabMaxAllocSize := runtime.GetGabMaxAllocSize()
	if gabMaxAllocSize != abi.MapMaxKeyBytes {
		t.Fatal("The threshold of GAB objects should be the same as the threshold of indirect map key in swiss map")
	}
}

type IndirectKey struct {
	arr [abi.MapMaxKeyBytes + 1]int
}

type IndirectValue struct {
	arr [abi.MapMaxElemBytes + 1]int
}

func makeIndirectKeyNonFrozenMap() map[IndirectKey]int {
	m := make(map[IndirectKey]int)
	return m
}

func makeIndirectElemFrozenMap() map[*int]IndirectValue {
	m := make(map[*int]IndirectValue)
	return m
}

func makeFrozenMap() map[*int]*IndirectValue {
	m := make(map[*int]*IndirectValue)
	return m
}

func makeNonFrozenMap() map[int]*IndirectValue {
	m := make(map[int]*IndirectValue)
	return m
}

func TestGabMapClone(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	if !isEvacSupported() {
		t.Skip("Skip balanced GC test on platforms other than linux/amd64 and darwin/amd64")
	}
	maxHeap := 4 << 30
	var envs = []string{
		fmt.Sprintf("MY_MEM_LIMIT=%d", maxHeap),
		"GODEBUG=gab=2,gcHeapPercent=50,gctrace=1",
		"GOGABOPT=evacuation=1,trace=1",
	}

	if !testGabInChildProcEnv(t, "GabMapClone", envs...) {
		return
	}

	idx := 0

	// Make a map with indirect key. This map bucket is not frozen.
	m0 := makeIndirectKeyNonFrozenMap()
	indirectKey := IndirectKey{}
	m0[indirectKey] = 0
	if runtime.CheckSwissMapFrozenBit(m0) {
		t.Fatal("The map bucket is expected not to be frozen.")
	}

	// Make a map with indirect value.
	m1 := makeIndirectElemFrozenMap()
	arr := IndirectValue{}
	m1[&idx] = arr
	if !runtime.CheckSwissMapFrozenBit(m1) {
		t.Fatal("The map bucket is expected to be frozen.")
	}
	clonedMap1 := maps.Clone(m1)
	if !runtime.CheckSwissMapFrozenBit(clonedMap1) {
		t.Fatal("The cloned map bucket is expected to be frozen.")
	}

	// 1. Make a map which is expected to be frozen.
	m2 := makeFrozenMap()
	arr = IndirectValue{}
	m2[&idx] = &arr
	if !runtime.CheckSwissMapFrozenBit(m2) {
		t.Fatal("The map bucket is expected to be frozen.")
	}
	clonedMap2 := maps.Clone(m2)
	if !runtime.CheckSwissMapFrozenBit(clonedMap2) {
		t.Fatal("The cloned map bucket is expected to be frozen.")
	}

	// 2. Make a non-frozen map. Both the original and cloned map
	// are expected NOT to be frozen. It is a non-frozen map
	// because its key is an integer.
	m3 := makeNonFrozenMap()
	if runtime.CheckSwissMapFrozenBit(m3) {
		t.Fatal("The map bucket is expected not to be frozen.")
	}
	// Test a map without any map buckets.
	clonedMap3 := maps.Clone(m3)
	if runtime.CheckSwissMapFrozenBit(clonedMap3) {
		t.Fatal("Cloning an empty map should be safe.")
	}

	// 3. Same as the previous case. Initialize the non-frozen map.
	arr = IndirectValue{}
	m3[idx] = &arr
	if runtime.CheckSwissMapFrozenBit(m3) {
		t.Fatal("The map bucket is expected not to be pinned.")
	}
	clonedMap3 = maps.Clone(m3)
	if runtime.CheckSwissMapFrozenBit(clonedMap3) {
		t.Fatal("The cloned map bucket is expected not to be pinned.")
	}
}

func TestGab2MemoryOrderingOnArm64(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("skipping on non-arm64")
	}

	testenv.MustHaveGoBuild(t)
	testenv.MustHaveExec(t)

	dir := t.TempDir()
	src := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	exe := "main"
	build := testenv.CleanCmdEnv(testenv.Command(t, testenv.GoToolPath(t), "build", "-o", exe, "main.go"))
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	cases := []struct {
		pattern string
		wants   []string
	}{
		{`runtime\.\(\*sweepLocked\)\.sweep`, []string{"LDAR"}},
		{`runtime\.\(\*mspan\)\.frozenBitsForIndex`, []string{"LDAR"}},
		{`runtime\.\(\*mspan\)\.newFrozenBits`, []string{"STLR"}},
		{`runtime\.\(\*mspan\)\.freeFrozenBits`, []string{"STLR"}},
	}

	var failed []string
	for _, c := range cases {
		objdump := testenv.CleanCmdEnv(testenv.Command(t, testenv.GoToolPath(t), "tool", "objdump", "-s", c.pattern, exe))
		objdump.Dir = dir
		out, err := objdump.CombinedOutput()
		if err != nil {
			t.Fatalf("go tool objdump failed for pattern %q: %v\n%s", c.pattern, err, out)
		}
		text := string(out)
		for _, want := range c.wants {
			if !strings.Contains(text, want) {
				failed = append(failed, fmt.Sprintf("pattern %q missing %q", c.pattern, want))
			}
		}
	}

	if len(failed) > 0 {
		t.Fatalf("objdump output does not contain expected instructions:\n  %s", strings.Join(failed, "\n  "))
	}
}
