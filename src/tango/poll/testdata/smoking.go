// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"fmt"
)

// Test: smoking test to verify build, linking, etc.
func main() {
	pd, err := register(-1)
	defer unregister(pd)
	fmt.Printf("%v\n", err)
}
