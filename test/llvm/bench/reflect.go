// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"reflect"
	"time"
)

type User struct {
	Name string
	Age  int
}

const iter = 100000

//go:llvm
//go:noinline
func BenchmarkLLVMreflectNew() {

	t1 := time.Now()
	var u *User
	uv := reflect.TypeOf(User{})
	for i := 0; i < iter; i++ {
		sn := reflect.New(uv)
		u, _ = sn.Interface().(*User)
	}

	_ = u
	t2 := time.Since(t1)
	fmt.Println("LLVM reflect new", t2)
}

//go:noinline
func BenchmarkreflectNew() {
	t1 := time.Now()
	var u *User
	uv := reflect.TypeOf(User{})
	for i := 0; i < iter; i++ {
		sn := reflect.New(uv)
		u, _ = sn.Interface().(*User)
	}

	_ = u
	t2 := time.Since(t1)
	fmt.Println("gc reflect new", t2)
}

//go:llvm
//go:noinline
func BenchmarkLLVMReflectset() {
	var u *User
	uv := reflect.TypeOf(User{})

	t1 := time.Now()
	for i := 0; i < iter; i++ {
		un := reflect.New(uv)
		u = un.Interface().(*User)
		u.Name = "hahaha"
		u.Age = 100
	}

	_ = u

	t2 := time.Since(t1)
	fmt.Println("llvm reflect set", t2)
}

func BenchmarkReflectset() {
	var u *User
	uv := reflect.TypeOf(User{})

	t1 := time.Now()
	for i := 0; i < iter; i++ {
		un := reflect.New(uv)
		u = un.Interface().(*User)
		u.Name = "hahaha"
		u.Age = 100
	}

	_ = u

	t2 := time.Since(t1)
	fmt.Println("gc reflect set", t2)
}

//go:llvm
//go:noinline
func BenchmarkLLVMReflectSetFieldByIndex() {
	uv := reflect.TypeOf(User{})

	t1 := time.Now()
	for i := 0; i < iter; i++ {
		sn := reflect.New(uv).Elem()
		sn.Field(0).SetString("xxx")
		sn.Field(1).SetInt(18)
	}

	t2 := time.Since(t1)
	fmt.Println("llvm reflect setfield by index:", t2)
}

//go:noinline
func BenchmarkReflectSetFieldByIndex() {
	uv := reflect.TypeOf(User{})

	t1 := time.Now()
	for i := 0; i < iter; i++ {
		sn := reflect.New(uv).Elem()
		sn.Field(0).SetString("xxx")
		sn.Field(1).SetInt(18)
	}

	t2 := time.Since(t1)
	fmt.Println("gc reflect setfield by index:", t2)
}

func main() {
	BenchmarkLLVMreflectNew()
	BenchmarkreflectNew()

	BenchmarkLLVMReflectset()
	BenchmarkReflectset()

	BenchmarkLLVMReflectSetFieldByIndex()
	BenchmarkReflectSetFieldByIndex()
}
