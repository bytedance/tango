// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func main() {
	foo()
}

//go:noinline
func foo() {

	messages := make(chan string)

	sendChan(messages)

	msg := <-messages

	if msg != "hello" {
		panic("channel crash")
	}

	return
}

//go:noinline
func sendChan(n chan string) {
	go func() { n <- "hello" }()
}
