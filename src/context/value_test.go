// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package context_test

import (
	. "context"
	"testing"
)

func valueTestCase() Context {
	ctx0 := Background()
	ctx1 := WithValue(ctx0, "1", 1)
	ctx2 := WithValue(ctx1, "2", 2)
	ctx3 := WithValue(ctx2, "3", 3)
	ctx4 := WithValue(ctx3, "4", 4)
	ctx5 := WithValue(ctx4, "5", 5)
	ctx6 := WithValue(ctx5, "6", 6)
	ctx7 := WithValue(ctx6, "7", 7)
	ctx8 := WithValue(ctx7, "8", 8)
	ctx9 := WithValue(ctx8, "9", 9)
	ctx10 := WithValue(ctx9, "10", 10)
	ctx11 := WithValue(ctx10, "11", 11)
	ctx12 := WithValue(ctx11, "12", 12)
	ctx13 := WithValue(ctx12, "13", 13)
	ctx14, _ := WithCancel(ctx13)
	ctx15, _ := WithCancel(ctx14)
	ctx16 := WithValue(ctx15, "16", 16)
	return ctx16
}

func TestContextValue(t *testing.T) {
	c := valueTestCase()
	if c.Value("7") != 7 {
		t.Error("failed to get the expect context value")
	}
	if c.Value("123") != nil {
		t.Error("an unexpect context value got")
	}
}

type CtxKey0 struct{}
type CtxKey1 struct{}
type CtxKey2 struct{}
type CtxKey3 struct{}
type CtxKey4 struct{}
type CtxKey5 struct{}
type CtxKey6 struct{}
type CtxKey7 struct{}
type CtxKey8 struct{}
type CtxKey9 struct{}
type CtxKeyInvalid struct{}

func TestWithEmptyStruct(t *testing.T) {
	ctx0 := Background()
	ctx1 := WithValue(ctx0, CtxKey0{}, 1)
	ctx2 := WithValue(ctx1, CtxKey1{}, 2)
	ctx3 := WithValue(ctx2, CtxKey2{}, 3)
	ctx4 := WithValue(ctx3, CtxKey3{}, 4)
	ctx5 := WithValue(ctx4, CtxKey4{}, 5)
	ctx6 := WithValue(ctx5, CtxKey5{}, 6)
	ctx7 := WithValue(ctx6, CtxKey6{}, 7)
	ctx8 := WithValue(ctx7, CtxKey7{}, 8)
	ctx9 := WithValue(ctx8, CtxKey8{}, 9)
	c := WithValue(ctx9, CtxKey9{}, 10)
	if c.Value(CtxKey6{}) != 7 {
		t.Error("failed to get the expect context value")
	}
	if c.Value("123") != nil {
		t.Error("an unexpect context value got")
	}
}
