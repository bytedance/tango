// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

var Cond1 bool
var Cond2 bool
var Cond3 bool
var Cond4 bool

func test(a int) {
	if a < 0 {
		print4()
		return
	}
	fmt.Println("depth", a)
	test(a - 1)
}

func print2() {
	if !Cond2 {
		print1()
	}
}

func print3() {
	if !Cond3 {
		print2()
	}
}

func print4() {
	if !Cond4 {
		print3()
	}
}

func print1() {
	if !Cond1 {
		printstack()
	}
}

//go:noinline
func printstack() {
	// debug.PrintStack()
	b := debug.Stack()
	checkStack(string(b))
}

func checkStack(stack string) {
	samples := strings.Split(sample, "\n")
	stacks := strings.Split(stack, "\n")
	if len(samples) != len(stacks) {
		fmt.Println(stack)
		fmt.Println("-----------------------------------------------")
		fmt.Println(sample)
		panic(fmt.Errorf("Different trace length:, expected %d, got %d", len(samples), len(stacks)))
	}
	for i := range samples {
		if !strings.Contains(stacks[i], samples[i]) {
			fmt.Println(stack)
			fmt.Println("-----------------------------------------------")
			fmt.Println(samples)
			panic(fmt.Errorf("Line %s does not contain %s", stacks[i], samples[i]))
		}
	}
}

func main() {
	test(5)
}

var sample string = `goroutine
runtime/debug.Stack(
runtime/debug/stack.go
main.printstack(
test/llvm/inlinestack.go:54
main.print1(
test/llvm/inlinestack.go:47
main.print2(
test/llvm/inlinestack.go:29
main.print3(
test/llvm/inlinestack.go:35
main.print4(
test/llvm/inlinestack.go:41
main.test(
test/llvm/inlinestack.go:20
main.test(
test/llvm/inlinestack.go:24
main.test(
test/llvm/inlinestack.go:24
main.test(
test/llvm/inlinestack.go:24
main.test(
test/llvm/inlinestack.go:24
main.test(
test/llvm/inlinestack.go:24
main.test(
test/llvm/inlinestack.go:24
main.main()
test/llvm/inlinestack.go:78
`
