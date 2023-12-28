// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package context

import (
	"errors"
	"fmt"
	"time"
)

func XTestValueTypeOfCancelledTimerCtx(t testingT) {
	ctx2, cancelCause := WithCancelCause(Background())
	ctx3, _ := WithTimeout(ctx2, 10*time.Second)
	ctx4 := WithValue(ctx3, "11111", "22222")
	ctx5, _ := WithCancel(ctx4)
	ctx := ctx5

	cancelCause(fmt.Errorf("cause_cancel"))
	err2 := Cause(ctx2)
	err3 := Cause(ctx3)
	err4 := Cause(ctx4)
	err5 := Cause(ctx5)
	err1 := Cause(ctx)

	if !errors.Is(err1, err2) || !errors.Is(err1, err3) || !errors.Is(err1, err4) || !errors.Is(err1, err5) {
		t.Fatalf("We should sync with upstream.")
	}
	if !errors.Is(ctx.Err(), ctx2.Err()) || !errors.Is(ctx.Err(), ctx3.Err()) || !errors.Is(ctx.Err(), ctx4.Err()) || !errors.Is(ctx.Err(), ctx5.Err()) {
		t.Fatalf("We should sync with upstream.")
	}
}
