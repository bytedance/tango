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

// Test: reader goroutine could be waken up with nested epolling.
func main() {
	p := &miniPoller{}
	defer p.finish()

	p.init()
	fdr, fdw, chr, _ := p.addPipe()

	go p.polling()

	done := make(chan bool)
	go func() {
		<-chr
		var buf [20]byte
		syscall.Read(fdr, buf[:])
		done <- true
	}()

	syscall.Write(fdw, []byte(testPacket))

	timer := time.NewTimer(2 * time.Second)
	select {
	case <-timer.C:
		fmt.Printf("Polling timed out, unexpected\n")
	case <-done:
		fmt.Printf("Reader finished as expected\n")
	}
}
