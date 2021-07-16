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

// Test: reader goroutine could be waken up and read successfully
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
		l, eread := syscall.Read(fdr, buf[:])
		if eread != nil {
			panic(eread)
		}
		if l > 0 {
			done <- true
		}
	}()

	syscall.Write(fdw, []byte(testPacket))

	timer := time.NewTimer(2 * time.Second)
	select {
	case <-timer.C:
		fmt.Printf("Polling timed out, unexpectedly\n")
	case <-done:
		fmt.Printf("Reader finished as expected\n")
	}
}
