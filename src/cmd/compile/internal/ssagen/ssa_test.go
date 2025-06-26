// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssagen

import (
	"cmd/internal/sys"
	"testing"
)

func TestUseDirectBoundsCheckFuncs(t *testing.T) {
	tests := []struct {
		name      string
		family    sys.ArchFamily
		tangoLLVM bool
		llvmMode  int
		want      bool
	}{
		{name: "wasm", family: sys.Wasm, want: true},
		{name: "standard", family: sys.AMD64},
		{name: "experiment only", family: sys.AMD64, tangoLLVM: true},
		{
			name:      "LLVM backend",
			family:    sys.AMD64,
			tangoLLVM: true,
			llvmMode:  2,
			want:      true,
		},
		{name: "LLVM mode without experiment", family: sys.AMD64, llvmMode: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := useDirectBoundsCheckFuncs(test.family, test.tangoLLVM, test.llvmMode)
			if got != test.want {
				t.Fatalf("useDirectBoundsCheckFuncs() = %v, want %v", got, test.want)
			}
		})
	}
}
