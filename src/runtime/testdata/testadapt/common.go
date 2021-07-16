// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	testPacket  = strings.Repeat("tango sdk ", 128)
	readEvents  = uint32(syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR | (-syscall.EPOLLET))
	writeEvents = uint32(syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR | (-syscall.EPOLLET))
)

// align with linux's 'struct epoll_event'
type epollEvent struct {
	events uint32
	data   [8]byte
}

// user-defined epoll data, we utilize channel to trigger waiting goroutine,
// for testing purpose.
type epollData struct {
	fd    int
	ready chan bool
}

// a 'poller' based on tango poll's nest epolling.
// caution: it's not multi-threaded safe, for testing purpose only
type miniPoller struct {
	epFd     int
	evFd     int
	epDesc   unsafe.Pointer
	epEvents []epollEvent
	epDatas  []*epollData
	sysFds   []int
}

func (p *miniPoller) init() {
	p.epFd, p.epDesc = createEpoll()

	// guarded descriptor for safely quitting
	if efd, _, err := syscall.Syscall(syscall.SYS_EVENTFD2, 0, 0, 0); err != 0 {
		panic(fmt.Errorf("Fail to create an event file, error: %v", err))
	} else {
		p.evFd = int(efd)
		p.sysFds = append(p.sysFds, p.evFd)
		p.control(p.evFd, syscall.EPOLL_CTL_ADD, readEvents, nil)
	}
}

func (p *miniPoller) control(fd int, op int, events uint32, ch chan bool) error {
	ev := epollEvent{}
	evd := &epollData{fd, ch}
	ev.events = events
	*(**epollData)(unsafe.Pointer(&ev.data)) = evd
	p.epDatas = append(p.epDatas, evd)

	_, _, en := syscall.RawSyscall6(syscall.SYS_EPOLL_CTL, uintptr(p.epFd), uintptr(op),
		uintptr(fd), uintptr(unsafe.Pointer(&ev)), 0, 0)
	if en == syscall.Errno(0) {
		return nil
	}
	return en
}

func (p *miniPoller) finish() error {
	_, err := syscall.Write(p.evFd, []byte{1})
	return err
}

func (p *miniPoller) cleanup() {
	unregister(p.epDesc)
	if p.epFd != 0 {
		syscall.Close(p.epFd)
	}
	for fd := range p.sysFds {
		syscall.Close(fd)
	}
}

func (p *miniPoller) wait() (int, error) {
	if len(p.epEvents) < len(p.sysFds) {
		p.epEvents = make([]epollEvent, len(p.sysFds))
	}

	var n uintptr
	var err error
	n, _, err = syscall.RawSyscall6(syscall.SYS_EPOLL_WAIT, uintptr(p.epFd),
		uintptr(unsafe.Pointer(&p.epEvents[0])), uintptr(len(p.epEvents)), 0, 0, 0)
	if err == syscall.Errno(0) {
		err = nil
	}
	return int(n), err
}

func (p *miniPoller) polling() {
	defer p.cleanup()

	for {
		n, err := p.wait()
		if err != nil && err != syscall.EINTR {
			return
		}
		if n > 0 {
			for i := 0; i < n; i++ {
				epd := *(**epollData)(unsafe.Pointer(&p.epEvents[i].data))
				if epd.fd == p.evFd {
					return
				}
				go func(epd *epollData) {
					epd.ready <- true
				}(epd)
			}
		} else if err := waitRead(p.epDesc); err != nil {
			return
		}
	}
}

// addPipe creates a pipe and register its read/write end to the nested epoll.
// The returned channels are for waking up the reader/writer goroutines separately,
// it simulates a basic scenario of how tango poll is expected to deploy.
func (p *miniPoller) addPipe() (fdr, fdw int, chr, chw chan bool) {
	var fds [2]int
	if err := syscall.Pipe2(fds[0:], syscall.O_CLOEXEC|syscall.O_NONBLOCK); err != nil {
		panic(err)
	}

	chr = make(chan bool)
	if err := p.control(fds[0], syscall.EPOLL_CTL_ADD, readEvents, chr); err != nil {
		panic(err)
	}

	chw = make(chan bool)
	if err := p.control(fds[1], syscall.EPOLL_CTL_ADD, writeEvents, chw); err != nil {
		panic(err)
	}
	p.sysFds = append(p.sysFds, fds[:]...)
	return fds[0], fds[1], chr, chw
}

func createEpoll() (fd int, pd unsafe.Pointer) {
	epfd, err := syscall.EpollCreate(1)
	if err != nil {
		panic(fmt.Errorf("Fail to create an epoll instance, error: %s", err))
	}

	epd, err := register(epfd)
	if err != nil {
		panic(fmt.Errorf("Registering external file failed, error: %s", err))
	}
	return epfd, epd
}

var padData [128]uint64

func stackGrowthRecursive(i int) {
	var pad [128]uint64
	pad = padData
	for j := range pad {
		if pad[j] != 0 {
			return
		}
	}
	if i != 0 {
		stackGrowthRecursive(i - 1)
	}
}
