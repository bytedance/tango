// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"reflect"
)

type User struct {
	Email string
	Name  string
	Age   int
}

func main() {
	reflect1()
	reflect2()
	//reflect3()
}

//go:noinline
func reflect1() {
	var u interface{} = User{}
	// TypeOf returns the reflection Type that represents the dynamic type of u.
	t := reflect.TypeOf(u)
	// Kind returns the specific kind of this type.
	if t.Kind() != reflect.Struct {
		fmt.Println("kind is not correct")
	}
}

//go:noinline
func reflect2() {
	u := &User{Name: "hahaha"}
	v := reflect.ValueOf(u).Elem()
	f := v.FieldByName("Name")

	if !f.IsValid() || !f.CanSet() {
		panic("field is not valid")
	}

	if f.Kind() != reflect.String {
		panic("reflect kind is not correct")
	}

	fmt.Println("f is", f)
	if f.Interface().(string) != "hahaha" {
		panic("value is not correct")
	}

	f.SetString("xxxx")
	if f.Interface().(string) != "xxxx" {
		panic("value is not set correctly")
	}
	fmt.Println("f is set to ", u.Name)
}

var num int

func (User) Hello() {
	num = 100
	fmt.Println("World")
}

//go:noinline
func reflect3() {
	v := reflect.ValueOf(User{})
	m := v.MethodByName("Hello")
	if m.Kind() != reflect.Func {
		panic("MethodByName expect reflect.Func")
	}
	m.Call(nil)

	if num != 100 {
		panic("reflect method not exected correctly?")
	}
}
