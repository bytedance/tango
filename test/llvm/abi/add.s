// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "textflag.h"

// func Add(x, y int64) int64
TEXT ·Add(SB), NOSPLIT, $0-24
    MOVQ x+0(FP), AX   // load x to AX
    MOVQ y+8(FP), BX   // load y to BX
    ADDQ AX, BX        // add x and y
    MOVQ BX, ret+16(FP) // store result
    RET
