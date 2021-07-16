// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"fmt"
	"math/rand"
	"strings"
	"syscall"
	"time"
)

// sanity testing for tango poll and adapt runtime scheduling,
// functionality wise.
func main() {
	p := &miniPoller{}
	defer p.finish()

	p.init()
	fdr, fdw, chr, chw := p.addPipe()

	go p.polling()

	// ensure the total bytes sent/received is larger than pipe capacity
	var packNum int
	var total int
	if pc, _, en := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fdr), syscall.F_GETPIPE_SZ, 0); en != 0 {
		fmt.Printf("Fail to get pipe capacity, error no: %v\n", en)
		return
	} else {
		factor := 20
		packNum = int(pc) / len(testPacket) * factor
		total = packNum * len(testPacket)
	}

	input := strings.Repeat(testPacket, int(packNum))
	output := make([]byte, total)
	done := make(chan bool)

	rand.Seed(time.Now().UnixNano())

	// reader
	received := 0
	waitr := 0
	go func() {
		for received < total {
			buf := make([]byte, rand.Intn(128))
			if l, e := syscall.Read(fdr, buf); e != syscall.EAGAIN {
				copy(output[received:], buf[0:l])
				received += l
				if received > total {
					fmt.Printf("Received packet exceeds limit\n")
					return
				}
			} else {
				waitr++
				<-chr
			}
		}

		done <- true
	}()

	// writer
	sent := 0
	waitw := 0
	go func() {
		for sent < total {
			if l, e := syscall.Write(fdw, []byte(testPacket)); e != syscall.EAGAIN {
				sent += l
				if sent > total {
					fmt.Printf("Sent packet exceeds limit\n")
					return
				}
			} else {
				waitw++
				<-chw
			}
		}
		done <- true
	}()

	// stack growth
	const N = 20
	for i := 0; i < N; i++ {
		go func() {
			stackGrowthRecursive(10)
			done <- true
		}()
	}

	timer := time.NewTimer(2 * time.Second)
	for i := 0; i < N+2; i++ {
		select {
		case <-timer.C:
			fmt.Printf("Timeout, testing failed\n")
			return
		case <-done:
		}
	}

	if input != string(output) {
		fmt.Printf("Sending/receiving packets mismatch\n")
		return
	}
	fmt.Printf("Finished reading %d bytes with %d waits\n", received, waitr)
	fmt.Printf("Finished sending %d bytes with %d waits\n", sent, waitw)
	fmt.Printf("All goroutines finished as expected\n")
}
