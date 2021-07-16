// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	_ "tango/poll"
	"unsafe"
)

//go:linkname register tango/poll.register
func register(fd int) (unsafe.Pointer, error)

//go:linkname unregister tango/poll.unregister
func unregister(pd unsafe.Pointer)

//go:linkname waitRead tango/poll.waitRead
func waitRead(pd unsafe.Pointer) error

//go:linkname waitWrite tango/poll.waitWrite
func waitWrite(pd unsafe.Pointer) error
