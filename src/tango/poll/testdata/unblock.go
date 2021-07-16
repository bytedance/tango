// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"fmt"
	"syscall"
	"time"
)

// Test: unregistering should unblock the waiting goroutine.
func main() {
	fdr, fdw, pdr, pdw := createRawPipe()
	defer func() {
		syscall.Close(fdw)
		unregister(pdw)
	}()

	done := make(chan bool)
	go func() {
		defer syscall.Close(fdr)
		if err := waitRead(pdr); err != nil {
			fmt.Printf("error: %v\n", err)
			done <- true
			return
		}
		var buf [20]byte
		l, _ := syscall.Read(fdr, buf[:])
		if l == 0 {
			done <- true
		}
	}()

	// ensure waitRead parks the reader goroutine
	time.Sleep(1 * time.Second)
	unregister(pdr)

	timer := time.NewTimer(2 * time.Second)
	select {
	case <-timer.C:
		fmt.Printf("Polling timed out, unexpectedly\n")
	case <-done:
		fmt.Printf("Reader finished as expected\n")
	}
}
