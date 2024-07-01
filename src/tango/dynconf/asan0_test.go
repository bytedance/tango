// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !asan

package dynconf

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSetLargeAllocTrace(t *testing.T) {
	defer func() {
		memdebugglobal = nil
	}()
	if os.Getenv("SUB_PROCESS") == "1" {
		var size int64 = 10 * 1024 * 1024
		fmt.Println("Alloc Not Record")
		allocMemory(size)

		res, err := cmdSetConfig([]byte(fmt.Sprintf("runtime.largeAllocTrace=%d", size)))
		if err != nil || !strings.Contains(string(res), "Successful") {
			t.Fatalf("set config failed %s", err)
			return
		}
		fmt.Println("Alloc Record", size)
		allocMemory(size)
		fmt.Println("Alloc Not Record")
		allocMemory(size - 1)

		res, err = cmdSetConfig([]byte(fmt.Sprintf("runtime.largeAllocTrace=%d", 0)))
		if err != nil || !strings.Contains(string(res), "Successful") {
			t.Fatalf("set config failed %s", err)
			return
		}
		fmt.Println("Alloc Not Record")
		allocMemory(size)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSetLargeAllocTrace$")
	cmd.Env = append(os.Environ(), "SUB_PROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("Return error: %s\n", err)
	}
	output := string(out)
	if success := checkLargeAllocTraceOutput(output); !success {
		t.Fatal("Not record memory allocation information")
	}
}
