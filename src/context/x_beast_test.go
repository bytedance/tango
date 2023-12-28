// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package context_test

import (
	. "context"
	"testing"
)

func TestValueTypeOfCancelledTimerCtx(t *testing.T) {
	XTestValueTypeOfCancelledTimerCtx(t)
}
