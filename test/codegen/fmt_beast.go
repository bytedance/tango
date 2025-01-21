// asmcheck -gcflags=-beast=fmtrewrite

// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen

import (
	"fmt"
	"reflect"
	"time"
)

type S string

func (s S) String() string {
	return "<" + string(s) + ">"
}

type E string

func (e E) Error() string {
	return "Error: " + string(e)
}

type X struct {
	S string
}

var hello = []byte("Hello!")

type Bool bool

type String string

type ErrorT string

func (e *ErrorT) Error() string {
	// amd64:`.*fmt\.Sprintf`
	// arm64:`.*fmt\.Sprintf`
	return fmt.Sprintf("%v", e)
}

// Format implements fmt.Formatter.
func (e *ErrorT) Format(f fmt.State, c rune) {}

func TestSprintf() {
	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	_ = fmt.Sprintf("%s,%s!", "Hello", "世界")
	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:`.*runtime\.fmt_itoa`
	// arm64:`.*runtime\.fmt_itoa`
	_ = fmt.Sprintf("Status %d %s", 200, "OK")
	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:`.*runtime\.fmt_uitoa`
	// arm64:`.*runtime\.fmt_uitoa`
	// amd64:`.*runtime\.fmt_hex`
	// arm64:`.*runtime\.fmt_hex`
	_ = fmt.Sprintf("%d %d %x", uint(123), 456, -1)
	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:`.*runtime\.fmt_stringer`
	// arm64:`.*runtime\.fmt_stringer`
	_ = fmt.Sprintf("%s", S("Hello!"))
	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:`.*time\.Duration\.String`
	// amd64:`.*time\.Duration\.String`
	_ = fmt.Sprintf("%v", time.Second)

	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:-`.*runtime\.slicebytetostring`
	// arm64:-`.*runtime\.slicebytetostring`
	// amd64:`.*runtime\.fmt_bool`
	// arm64:`.*runtime\.fmt_bool`
	_ = fmt.Sprintf("%s %t", hello, true)
	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	_ = fmt.Sprintf("%v-%v-%v", 123, "456", false)
	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:`.*runtime\.fmt_error`
	// arm64:`.*runtime\.fmt_error`
	_ = fmt.Sprintf("%v", E("Not Found!"))

	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:`.*runtime\.fmt_bool`
	// arm64:`.*runtime\.fmt_bool`
	_ = fmt.Sprintf("%v", Bool(true))

	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	// amd64:`.*runtime\.fmt_bool`
	// arm64:`.*runtime\.fmt_bool`
	_ = fmt.Sprintf("%t", Bool(true))

	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	_ = fmt.Sprintf("%v world!", String("hello"))

	// amd64:-`.*fmt\.Sprintf`
	// arm64:-`.*fmt\.Sprintf`
	_ = fmt.Sprintf("%s world!", String("hello"))

	// amd64:`.*fmt\.Sprintf`
	// arm64:`.*fmt\.Sprintf`
	_ = fmt.Sprintf("%v", reflect.ValueOf(1)) // don't optimize reflect.Value

	a := []any{"before", "world"}
	a[0] = "after"
	// amd64:`.*fmt\.Sprintf`
	// arm64:`.*fmt\.Sprintf`
	_ = fmt.Sprintf("%v %v", a...)
}
