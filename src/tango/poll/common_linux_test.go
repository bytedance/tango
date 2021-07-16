// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64
// +build amd64

package poll

import (
	"internal/testenv"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

const source = `
package main
import (
	"fmt"
)
func main() {
	fmt.Printf("hello world\n")
}`

func tempFile(t *testing.T, fn string) string {
	fullpath := filepath.Join(t.TempDir(), fn)
	err := ioutil.WriteFile(fullpath, []byte(source), 0666)
	if err != nil {
		t.Fatalf("Fail to create %s: %v\n", fullpath, err)
	}
	return fullpath
}

func runTestfile(t *testing.T, fn string, shouldFail bool, want ...string) ([]byte, error) {
	testenv.MustHaveGoRun(t)
	cmd := exec.Command(testenv.GoToolPath(t), "run",
		filepath.Join("testdata", "link.go"),
		filepath.Join("testdata", "common.go"),
		filepath.Join("testdata", fn))
	out, err := cmd.CombinedOutput()

	if !shouldFail && err != nil {
		t.Fatalf("Unexpected failure, file: %s, error: '%v', out: %s", fn, err, string(out))
	}
	if shouldFail && err == nil {
		t.Fatalf("Expecting a fatal error, file: %s", fn)
	}

	for _, w := range want {
		if !strings.Contains(string(out), w) {
			t.Fatalf("output mismatches, want: '%s', got: '%s'", w, string(out))
		}
	}
	// for further processing if necessary
	return out, err
}

func createRawPipe(t *testing.T) (fdr, fdw int, pdr, pdw unsafe.Pointer) {
	var fds [2]int

	err := syscall.Pipe2(fds[0:], syscall.O_CLOEXEC|syscall.O_NONBLOCK)
	if err != nil {
		t.Fatalf("Fail to create a pipe, error: %v", err)
	}

	pd1, e1 := register(fds[0])
	if e1 != nil {
		t.Fatalf("Fail to register a pollable descriptor, error: %v", e1)
	}

	pd2, e2 := register(fds[1])
	if e2 != nil {
		t.Fatalf("Fail to register a pollable descriptor, error: %v", e2)
	}
	return fds[0], fds[1], pd1, pd2
}

func createEpoll(t *testing.T) (fd int, pd unsafe.Pointer) {
	epfd, err := syscall.EpollCreate(1)
	if err != nil {
		t.Fatalf("Fail to create an epoll instance, error: %v", err)
	}

	epd, err := register(epfd)
	if err != nil {
		t.Fatalf("Fail to register a nested epoll instance, error: %v", err)
	}
	return epfd, epd
}
