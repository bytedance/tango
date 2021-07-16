// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// We isolate the following testcases by separate 'go run' processes if necessary
// to avoid interfering with the runtime running the test flow, as some tests may
// drive the runtime into unexpected/unrecoverable status which could be rescued
// by process termination only.
// Pick up a few randomly to run in parallel.

//go:build amd64
// +build amd64

package poll

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var (
	testPacket = "tango sdk "
)

func TestSmoke(t *testing.T) {
	// test building, linking etc.
	runTestfile(t, "smoking.go", false, "bad file descriptor")
}

func TestDoubleRd(t *testing.T) {
	// multiple waiting with the same mode should panic runtime
	runTestfile(t, "double_read.go", true, "runtime: double wait")
}

func TestDoubleWr(t *testing.T) {
	// multiple waiting with the same mode should panic runtime
	runTestfile(t, "double_write.go", true, "runtime: double wait")
}

func TestEmpty(t *testing.T) {
	// basic testing for epoll_wait
	runTestfile(t, "empty.go", false, "Polling timed out")
}

func TestClosed(t *testing.T) {
	// closing should generate a readable event (RDHUP) for the peer end
	runTestfile(t, "closed.go", false, "Reader finished as expected")
}

func TestUnblock(t *testing.T) {
	// unregistering a descriptor unblocks its waiting goroutine to return
	// a polling error
	runTestfile(t, "unblock.go", false, "Reader finished as expected", "use of closed file")
}

func TestDoubleUnreg(t *testing.T) {
	// unregistering the same descriptor more than once is forbidden
	runTestfile(t, "double_unreg.go", true, "unblock on closing polldesc")
}

func TestUnreg(t *testing.T) {
	// waiting on an unregistered descriptor is erronous
	t.Parallel()
	epfd, epd := createEpoll(t)
	defer syscall.Close(epfd)
	unregister(epd)
	err := waitRead(epd)
	if err == nil || !strings.Contains(err.Error(), "use of closed file") {
		t.Errorf("Expecting an error, got: %v", err)
	}
}

func TestDoubleReg(t *testing.T) {
	// registering the same descriptor more than once is erronous
	epfd, epd := createEpoll(t)
	defer syscall.Close(epfd)
	defer unregister(epd)

	pd, err := register(epfd)
	defer unregister(pd)
	if err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("Expecting an error, got: %v", err)
	}
}

func TestRegular(t *testing.T) {
	// Regular linux disk file is not pollable
	var fd int
	tf := tempFile(t, "test.go")
	fd, err := syscall.Open(tf, syscall.O_RDWR|syscall.O_CLOEXEC, uint32(os.FileMode(0777).Perm()))
	if err != nil {
		t.Fatalf("Fail to open test file %s for testing, error: %s", tf, err)
	}
	defer syscall.Close(fd)

	pd, err := register(fd)
	defer unregister(pd)
	if err == nil || !strings.Contains(err.Error(), "operation not permitted") {
		t.Fatalf("Expecting an EPERM error, got: %v", err)
	}
}

// We fabricate a few cases utilizing raw pipe file descriptors to check that
// tango poll works, but they are barely able to demonstrate the internal
// subtle difference of runtime scheduling w./w.o. tango poll being employed,
// so we just focus on functionality verification.

func TestRead(t *testing.T) {
	// a reader goroutine could be waken up (or is not blocked)
	runTestfile(t, "read.go", false, "Reader finished")
}

func TestReadBlock(t *testing.T) {
	// a reader goroutine is waken up once and only once
	runTestfile(t, "read_block.go", false, "Polling timed out")
}

func TestNestRead(t *testing.T) {
	// a reader goroutine monitored by nested epoll could be
	// waken up (or is not blocked)
	t.Parallel()
	runTestfile(t, "nest_read.go", false, "Reader finished")
}

func TestNestReadBlock(t *testing.T) {
	// a reader goroutine monitored by nested epoll is waken
	// up once and only once
	runTestfile(t, "nest_read_block.go", false, "Polling timed out")
}

func TestNestRW(t *testing.T) {
	// both read/write polling work with nested epoll
	runTestfile(t, "nest_read_write.go", false, "Reader/Writer finished")
}

func TestRW(t *testing.T) {
	// both read/write polling work with runtime netpolling
	t.Parallel()
	fdr, fdw, pdr, pdw := createRawPipe(t)
	defer func() {
		syscall.Close(fdr)
		syscall.Close(fdw)
		unregister(pdr)
		unregister(pdw)
	}()

	// ensure the total bytes sent/received is larger than pipe capacity
	var packNum int
	if pc, _, en := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fdr), syscall.F_GETPIPE_SZ, 0); en != 0 {
		t.Skipf("Fail to get pipe capacity, error no: %v", en)
	} else {
		factor := 10
		packNum = int(pc) / len(testPacket) * factor
	}

	total := packNum * len(testPacket)
	input := strings.Repeat(testPacket, int(packNum))
	output := make([]byte, total)
	done := make(chan bool)

	// reader
	received := 0
	waitr := 0
	buf := make([]byte, 128)
	go func() {
		for received < total {
			if l, e := syscall.Read(fdr, buf); e != syscall.EAGAIN {
				copy(output[received:], buf[0:l])
				received += l
				if received > total {
					t.Errorf("Received packet exceeds limit")
					return
				}
			} else {
				waitr++
				waitRead(pdr)
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
					t.Errorf("Sent packet exceeds limit")
					return
				}
			} else {
				waitw++
				waitWrite(pdw)
			}
		}
		done <- true
	}()

	timer := time.NewTimer(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-timer.C:
			t.Fatalf("Polling timed out, unexpectedly")
		case <-done:
		}
	}

	if input != string(output) {
		t.Fatalf("Sending/receiving packets mismatch")
	}

	t.Logf("Finished reading %d bytes with %d waits", received, waitr)
	t.Logf("Finished sending %d bytes with %d waits", sent, waitw)
}

func TestMassiveOpen(t *testing.T) {
	// stress testing the runtime's ability to support massive polling descriptors
	// It's a positive testing any way, seems no need to test the system's upper limit.
	const N = 1024 * 8
	const initFD = int(-1)
	fds := make([]int, N*2)
	pds := make([]unsafe.Pointer, N*2)

	for i := 0; i < N; i++ {
		fds[i*2], fds[i*2+1] = initFD, initFD
	}

	defer func() {
		for i := 0; i < N; i++ {
			unregister(pds[i*2])
			unregister(pds[i*2+1])
			if fds[i*2] != initFD {
				syscall.Close(fds[i*2])
			}
			if fds[i*2+1] != initFD {
				syscall.Close(fds[i*2+1])
			}
		}

	}()

	for i := 0; i < N; i++ {
		fds[i*2], fds[i*2+1], pds[i*2], pds[i*2+1] = createRawPipe(t)
	}
}
