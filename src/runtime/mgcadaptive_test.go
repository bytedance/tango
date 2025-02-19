// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	. "runtime"
	"strconv"
	"testing"
	_ "unsafe"
)

const (
	maxUint64 = ^uint64(0)
	maxInt64  = int64(maxUint64 >> 1)
)

var (
	gcAdaptiveSink interface{}
)

//go:linkname setGCAdaptiveFunc runtime.setGCAdaptiveFunc
var setGCAdaptiveFunc func(enable bool)

//go:linkname setGCThroughputGoalFunc runtime.setGCThroughputGoalFunc
var setGCThroughputGoalFunc func(throughput float64)

//go:linkname setGCAdaptiveFuncNotExist runtime.setGCAdaptiveFuncNotExist
var setGCAdaptiveFuncNotExist func(bytes int64) int64

//go:linkname setGCThroughputGoalFuncNotExist runtime.setGCThroughputGoalFuncNotExist
var setGCThroughputGoalFuncNotExist func(throughput float64)

// Check if the function setGCAdaptiveFunc is linked correctly
func TestSymbolExportedGCAdaptive(t *testing.T) {
	if setGCAdaptiveFuncNotExist != nil {
		t.Errorf("linkname a function that does not exist return not nil")
	}

	if setGCAdaptiveFunc == nil {
		t.Errorf("linkname an existing function return nil")
	}

	if setGCThroughputGoalFuncNotExist != nil {
		t.Errorf("linkname a function that does not exist return not nil")
	}

	if setGCThroughputGoalFunc == nil {
		t.Errorf("linkname an existing function return nil")
	}
}

func TestCalculateAdjustmentStep(t *testing.T) {
	tests := []struct {
		name     string
		delta    float64
		base     float64
		expected float64
	}{
		{
			name:     "delta is zero",
			delta:    0,
			base:     GCThroughputDeltaBase,
			expected: MinGOGCAdjustmentStep,
		},
		{
			name:     "delta equals base (max step)",
			delta:    GCThroughputDeltaBase,
			base:     GCThroughputDeltaBase,
			expected: MaxGOGCAdjustmentStep,
		},
		{
			name:     "delta is half of base (mid step)",
			delta:    GCThroughputDeltaBase / 2,
			base:     GCThroughputDeltaBase,
			expected: (MinGOGCAdjustmentStep + MaxGOGCAdjustmentStep) / 2,
		},
		{
			name:     "delta above base (clamped to max)",
			delta:    GCThroughputDeltaBase * 2,
			base:     GCThroughputDeltaBase,
			expected: MaxGOGCAdjustmentStep,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := ComputeAdjustmentStep(tt.delta, tt.base)
			if math.Abs(actual-tt.expected) > 1e-6 {
				t.Errorf("test name %s: expected %.6f, got %.6f", tt.name, tt.expected, actual)
			}
		})
	}
}

func checkGCAdaptiveState(t *testing.T, expected bool) {
	state := CheckGCAdaptiveIsEnabled()
	if state != expected {
		t.Fatalf("gc adaptive state should be %v, got %v.", expected, state)
	}
}

func checkGCPercent(t *testing.T, expected int32, gcPercent int32) {
	if gcPercent != expected {
		t.Fatalf("gc percent should be %v, got %v.", expected, gcPercent)
	}
}

func checkGCPercentInRange(t *testing.T, gcPercent int32, scope [2]int32) {
	if gcPercent < scope[0] || gcPercent > scope[1] {
		t.Fatalf("gc percent should be in range %v, got %v.", scope, gcPercent)
	}
}

func checkMemoryLimit(t *testing.T, expected int64, memoryLimit int64) {
	if memoryLimit != expected {
		t.Fatalf("initial memory limit should be %v, got %v.", expected, memoryLimit)
	}
}

func checkGOGCBoundRange(t *testing.T, lowerBound int32, upperBound int32) {
	if lowerBound != GCAdaptiveGOGCLowerLimit() || upperBound != GCAdaptiveGOGCUpperLimit() {
		t.Fatalf("GOGC bound should be %v, got %v.", [2]int32{GCAdaptiveGOGCLowerLimit(), GCAdaptiveGOGCUpperLimit()}, [2]int32{lowerBound, upperBound})
	}
}

func checkGCThroughputBoundRange(t *testing.T, lowerBound float64, upperBound float64) {
	if math.Abs(lowerBound-GCThroughputLowerGoal()) > 1e-6 || math.Abs(upperBound-GCThroughputUpperGoal()) > 1e-6 {
		t.Fatalf("GC throughput bound should be %v, got %v.", [2]float64{GCThroughputLowerGoal(), GCThroughputUpperGoal()}, [2]float64{lowerBound, upperBound})
	}
}

func checkGCThroughputInRange(t *testing.T, gcThroughput float64, scope [2]float64) {
	if gcThroughput < scope[0] || gcThroughput > scope[1] {
		t.Fatalf("gc throughput should be in range %v, got %v.", scope, gcThroughput)
	}
}

func launchSubProcess(t *testing.T, testFuncName string, envVars ...string) {
	var outb, errb bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^"+testFuncName+"$")
	cmd.Env = append(os.Environ(), envVars...)
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		t.Log(cmd.Stderr)
		t.Log(cmd.Stdout)
		t.Fatalf("Return error: %s\n", err)
	}
}

// Case one: defualt env with gc adaptive is on and GOGCUPPERLIMIT = 150
func TestGCAdaptiveWithDefaultEnv(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		checkGOGCBoundRange(t, 50, ReadGOGCUPPERLIMIT())
		checkGCThroughputBoundRange(t, 94.2875, 95.7125)
		for {
			gcAdaptiveSink = make([]byte, 1<<20)
			if CheckGCIsStable() {
				break
			}
		}
		// expect to trigger gc happen.
		gcNum := GetGCNum()
		for GetGCNum()-gcNum < 20 {
			gcAdaptiveSink = make([]byte, 1<<20)
		}
		checkGCPercent(t, 100, InitialGCPercent())
		checkMemoryLimit(t, maxInt64, InitialMemoryLimit())
		checkGCPercent(t, ReadGOGCUPPERLIMIT(), GetGCPercent())
		return
	}
	launchSubProcess(t, "TestGCAdaptiveWithDefaultEnv", "SUB_PROCESS=1", "GOMAXPROCS=4", "GOGCUPPERLIMIT=150", "GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}

// Case two: GOGC=off with gc adaptive is on and GOGCUPPERLIMIT = 150
func TestGCAdaptiveWithGOGCOff(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		checkGOGCBoundRange(t, 50, ReadGOGCUPPERLIMIT())
		checkGCThroughputBoundRange(t, 94.2875, 95.7125)
		checkGCPercent(t, GCAdaptiveDefaultGCPercent(), GetGCPercent())
		for {
			gcAdaptiveSink = make([]byte, 1<<20)
			if CheckGCIsStable() {
				break
			}
		}
		// expect to trigger gc happen.
		gcNum := GetGCNum()
		for GetGCNum()-gcNum < 20 {
			gcAdaptiveSink = make([]byte, 1<<20)
		}
		checkGCPercent(t, -1, InitialGCPercent())
		checkGCPercent(t, GetGCPercent(), ReadGOGCUPPERLIMIT())
		checkGCThroughputInRange(t, GCThroughputEMA(), [2]float64{20.0, 94.0})
		return
	}
	launchSubProcess(t, "TestGCAdaptiveWithGOGCOff", "SUB_PROCESS=1", "GOMAXPROCS=4", "GOGCUPPERLIMIT=150", "GOGC=off", "GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}

func TestGCAdaptiveSwitch(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		// Check the initial state of GC adaptive
		checkGCAdaptiveState(t, false)
		checkGCPercent(t, 400, GetGCPercent())
		checkGCPercent(t, 400, InitialGCPercent())
		checkGOGCBoundRange(t, 50, 400)
		checkGCThroughputBoundRange(t, 59.55, 60.45)
		// Enable GC adaptive.
		SetGCAdaptiveEnabled(1)
		checkGCAdaptiveState(t, true)
		// Make sure the GC state is stable.
		for {
			gcAdaptiveSink = make([]byte, 1<<20)
			if CheckGCIsStable() {
				break
			}
		}
		gcNum := GetGCNum()
		for GetGCNum()-gcNum < 15 {
			gcAdaptiveSink = make([]byte, 1<<20)
		}
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GCAdaptiveGOGCLowerLimit(), GCAdaptiveGOGCUpperLimit()})
		SetGCThroughputGoal(95.0)
		// Make sure gc througput is updated.
		GC()
		checkGCThroughputBoundRange(t, 94.2875, 95.7125)
		gcNum = GetGCNum()
		for GetGCNum()-gcNum < 30 {
			gcAdaptiveSink = make([]byte, 1<<20)
		}
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GCAdaptiveGOGCUpperLimit() - 100, GCAdaptiveGOGCUpperLimit()})
		SetGCAdaptiveEnabled(0)
		checkGCAdaptiveState(t, false)
		// GOGC should be restore to 400.
		checkGCPercent(t, 400, GetGCPercent())
		return
	}
	launchSubProcess(t, "TestGCAdaptiveSwitch", "SUB_PROCESS=1", "GOMAXPROCS=4", "GOGC=400", "GOGCTHPRATIO=60", "GODEBUG=gctrace=1,gcadaptivetrace=1")
}

func TestGCAdaptiveWithMaxHeapOn(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		checkGOGCBoundRange(t, 50, 100)
		checkGCThroughputBoundRange(t, 94.2875, 95.7125)
		checkGCPercent(t, 100, GetGCPercent())
		// Make sure the GC state is stable.
		for {
			gcAdaptiveSink = make([]byte, 1<<20)
			if CheckGCIsStable() {
				break
			}
		}
		gcNum := GetGCNum()
		for GetGCNum()-gcNum < 5 {
			gcAdaptiveSink = make([]byte, 1<<20)
		}
		checkGCPercent(t, 100, InitialGCPercent())
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GCAdaptiveDefaultGCPercent(), 100000})
		return
	}
	launchSubProcess(t, "TestGCAdaptiveWithMaxHeapOn", "SUB_PROCESS=1", "GOMAXPROCS=4", "GOGCTHPRATIO=95", "MY_MEM_LIMIT="+strconv.Itoa(400<<20), "GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}
