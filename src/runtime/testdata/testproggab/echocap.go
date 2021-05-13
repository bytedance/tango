// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
)

func init() {
	register("EchoMaxCap", echoMaxCap)
}

func echoMaxCap() {
	_, max := getGabCap()
	fmt.Printf("%d", max)
}
