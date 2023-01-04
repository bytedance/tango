// asmcheck

// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codegen tests whether the code is generated as expected
package codegen

import (
	"fmt"
	"math"
)

func FMAInst() {
	// amd64/v1:`.*runtime\.x86HasFMA`
	// amd64/v2:`.*runtime\.x86HasFMA`
	for _, tt := range []struct {
		x, y, z, want float64
	}{
		{2, 3, 4, 10},
		{3, 4, 5, 17},
	} {
		// amd64/v1: `*math.FMA*`
		// amd64/v2: `*math.FMA*`
		// amd64/v1: `*VFMADD*`
		// amd64/v2: `*VFMADD*`
		if got := math.FMA(tt.x, tt.y, tt.z); got != tt.want {
			fmt.Println("FMA(%f,%f,%f) = %f, want %f", tt.x, tt.y, tt.z, got, tt.want)
		}
	}
}
