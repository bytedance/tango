// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"reflect"
	"runtime"
	"testing"
)

func TestTangoMemBits(t *testing.T) {
	if reflect.TypeOf(runtime.TangoMemmoveBits).Kind() != reflect.Uint8 {
		t.Fatalf("runtime.memmovebits is expected to be of type uint8.")
	}
	if (runtime.TangoAVXSupported&1) != 1 ||
		(runtime.TangoAVXSupported>>1) != 0 {
		t.Fatal("The low bit of runtime.avxSupported should be 1.")
	}
}
