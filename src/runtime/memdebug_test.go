// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

var memdebugglobal interface{}

func allocA(size int64) {
	allocB(size)
}

func allocB(size int64) {
	allocC(size)
}

func allocC(size int64) {
	memdebugglobal = make([]byte, size)
}

func TestMemDebugRecord(t *testing.T) {
	defer func() {
		memdebugglobal = nil
	}()
	if os.Getenv("SUB_PROCESS") == "1" {
		allocA(10 * 1024 * 1024)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMemDebugRecord")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "GODEBUG=largealloctrace="+strconv.Itoa(10*1024*1024))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output := string(out)
	if !strings.Contains(output, "largeAllocObject bytes=10485760") {
		t.Fatal("Not record memory allocation information")
	}
	if !strings.Contains(output, "runtime_test.allocC") || !strings.Contains(output, "runtime_test.allocB") ||
		!strings.Contains(output, "runtime_test.allocA") {
		t.Fatal("Memory allocation call stack information error")
	}
}

func TestMemDebugNotRecord(t *testing.T) {
	defer func() {
		memdebugglobal = nil
	}()
	if os.Getenv("SUB_PROCESS") == "1" {
		allocA(10 * 1024 * 1023)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMemDebugNotRecord")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "GODEBUG=largealloctrace="+strconv.Itoa(10*1024*1024))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output := string(out)
	if strings.Contains(output, "largeAllocObject") {
		t.Fatal("Record memory allocations that do not reach the threshold")
	}
}

func TestMemDebugBound(t *testing.T) {
	defer func() {
		memdebugglobal = nil
	}()
	if os.Getenv("SUB_PROCESS") == "1" {
		var allocSize int64 = 10 * 1024 * 1024
		if allocSizeStr := os.Getenv("ALLOCSIZE"); allocSizeStr != "" {
			if value, err := strconv.ParseInt(allocSizeStr, 10, 64); err == nil {
				allocSize = value
			}
		}
		allocA(allocSize)
		return
	}

	// allocsize = 10,485,760 GODEBUG=largealloctrace=0, 0<=0, will not record.
	cmd := exec.Command(os.Args[0], "-test.run=^TestMemDebugBound")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "GODEBUG=largealloctrace="+strconv.Itoa(0))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output := string(out)
	if strings.Contains(output, "largeAllocObject") {
		t.Fatal("Record memory allocations when disable")
	}

	// allocsize = 10,485,760 GODEBUG=largealloctrace=-1, -1<=0, will not record.
	cmd = exec.Command(os.Args[0], "-test.run=^TestMemDebugBound")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "GODEBUG=largealloctrace="+strconv.Itoa(-1))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output = string(out)
	if strings.Contains(output, "largeAllocObject") {
		t.Fatal("Record memory allocations when disable")
	}

	// allocsize = math.MaxInt32+5 GODEBUG=largealloctrace=math.MaxInt32+1, math.MaxInt32+1 > math.MaxInt32, will not record.
	cmd = exec.Command(os.Args[0], "-test.run=^TestMemDebugBound")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "ALLOCSIZE="+strconv.FormatInt(math.MaxInt32+5, 10), "GODEBUG=largealloctrace="+strconv.Itoa(math.MaxInt32+1))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output = string(out)
	if strings.Contains(output, "largeAllocObject") {
		t.Fatal("Record memory allocations when disable")
	}

	// allocsize = 1024 GODEBUG=largealloctrace=1, 1024B < 32 KB, will not record.
	cmd = exec.Command(os.Args[0], "-test.run=^TestMemDebugBound")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "ALLOCSIZE="+strconv.FormatInt(1024, 10), "GODEBUG=largealloctrace="+strconv.Itoa(1))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output = string(out)
	if strings.Contains(output, "largeAllocObject") {
		t.Fatal("Record memory allocations when disable")
	}

	// allocsize = 10,485,760 GODEBUG=largealloctrace=1, 10485760B > 32 KB && 10485760B > 1B, should record.
	cmd = exec.Command(os.Args[0], "-test.run=^TestMemDebugBound")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "GODEBUG=largealloctrace="+strconv.Itoa(1))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output = string(out)
	if !strings.Contains(output, "largeAllocObject bytes=10485760") {
		t.Fatal("Not record memory allocation information")
	}
	if !strings.Contains(output, "runtime_test.allocC") || !strings.Contains(output, "runtime_test.allocB") ||
		!strings.Contains(output, "runtime_test.allocA") {
		t.Fatal("Memory allocation call stack information error")
	}

	// allocsize = math.MaxInt32+5 GODEBUG=math.MaxInt32, math.MaxInt32+5 > math.MaxInt32, should record.
	cmd = exec.Command(os.Args[0], "-test.run=^TestMemDebugBound")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "ALLOCSIZE="+strconv.FormatInt(math.MaxInt32+5, 10), "GODEBUG=largealloctrace="+strconv.Itoa(math.MaxInt32))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output = string(out)
	if !strings.Contains(output, "largeAllocObject bytes="+strconv.FormatInt(math.MaxInt32+5, 10)) {
		t.Fatal("Not record memory allocation information")
	}
	if !strings.Contains(output, "runtime_test.allocC") || !strings.Contains(output, "runtime_test.allocB") ||
		!strings.Contains(output, "runtime_test.allocA") {
		t.Fatal("Memory allocation call stack information error")
	}
}

//go:noinline
func GenericAdd[T int | float32 | float64](a T, b T) T {
	memdebugglobal = make([]byte, 1024*1024)
	return a + b
}

func TestMemDebugGenericFunc(t *testing.T) {
	defer func() {
		memdebugglobal = nil
	}()
	if os.Getenv("SUB_PROCESS") == "1" {
		GenericAdd(1, 2)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMemDebugGenericFunc")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1", "GODEBUG=largealloctrace="+strconv.Itoa(1024*1024))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output := string(out)
	if !strings.Contains(output, "largeAllocObject bytes=1048576") {
		t.Fatal("Not record memory allocation information")
	}
	if !strings.Contains(output, "runtime_test.GenericAdd[go.shape.int]") {
		t.Fatal("Memory allocation call stack information error")
	}
}
