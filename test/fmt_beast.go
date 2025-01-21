// errorcheck -0 -d=fmtrewrite -beast=fmtrewrite

// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

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

func main() {
	_ = fmt.Sprintf("%d", -1)             // ERROR "intrinsic fmt.Sprintf"
	_ = fmt.Sprintf("%v", S(""))          // ERROR "intrinsic fmt.Sprintf"
	_ = fmt.Sprintf("%v", E(""))          // ERROR "intrinsic fmt.Sprintf"
	_, _ = fmt.Sprintf("%d", -1), false   // ERROR "intrinsic fmt.Sprintf"
	_ = fmt.Sprintf("%d", -1)[:1]         // ERROR "intrinsic fmt.Sprintf"
	_ = fmt.Sprintf("%d", -1)[1]          // ERROR "intrinsic fmt.Sprintf"
	_ = fmt.Sprintf("%t", false) + "true" // ERROR "intrinsic fmt.Sprintf"
	_ = S(fmt.Sprintf("%d", -1))          // ERROR "intrinsic fmt.Sprintf"
	nop(fmt.Sprintf("%d", 1))             // ERROR "intrinsic fmt.Sprintf"

	_ = X{
		S: fmt.Sprintf("%d", 0xff), // ERROR "intrinsic fmt.Sprintf"
	}

	_ = map[string]string{
		"0":                  fmt.Sprintf("%d", 0), // ERROR "intrinsic fmt.Sprintf"
		fmt.Sprintf("%d", 1): "1",                  // ERROR "intrinsic fmt.Sprintf"
	}

	switch "true" {
	case fmt.Sprintf("%t", true): // ERROR "intrinsic fmt.Sprintf"
	}

	for range fmt.Sprintf("%d", 20231108) { // ERROR "intrinsic fmt.Sprintf"
	}
}

func foo() string {
	return fmt.Sprintf("%x", -2023) // ERROR "intrinsic fmt.Sprintf"
}

func bar() {
	panic(fmt.Sprintf("%s%d", "oops", 2023)) // ERROR "intrinsic fmt.Sprintf"
}

func nop(s string) {}
