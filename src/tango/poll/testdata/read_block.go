// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// Test: reader goroutine is waken up once and only once
func main() {
	fdr, fdw, pdr, pdw := createRawPipe()
	defer func() {
		syscall.Close(fdw)
		unregister(pdw)
	}()

	done := make(chan bool)
	go func() {
		defer func() {
			syscall.Close(fdr)
			unregister(pdr)
		}()
		if err := waitRead(pdr); err != nil {
			fmt.Printf("error: %v\n", err)
			os.Exit(1)
		}
		var buf [20]byte
		syscall.Read(fdr, buf[:])
		if err := waitRead(pdr); err != nil {
			fmt.Printf("error: %v\n", err)
			os.Exit(1)
		}
		done <- true
	}()

	syscall.Write(fdw, []byte(testPacket))

	timer := time.NewTimer(2 * time.Second)
	select {
	case <-timer.C:
		fmt.Printf("Polling timed out as expected\n")
	case <-done:
		fmt.Printf("waitRead finished, unexpectedly\n")
	}
}
