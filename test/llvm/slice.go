// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"time"
)

const BenchMarkSize = 10000
const iter = 10000

func generateIntSlice() []int {
	var m = make([]int, BenchMarkSize)
	for i := 0; i < BenchMarkSize; i++ {
		m[i] = i
	}
	return m
}

func checkItem(index, item int) {
	if item != index {
		panic("not equal")
	}
}

//go:noinline
func llvmSliceReadRange(m []int) {
	for n := 0; n < 10000; n++ {
		for i, e := range m {
			checkItem(i, e)
		}
	}
}

//go:noinline
func SliceReadRange(m []int) {
	for n := 0; n < 10000; n++ {
		for i, e := range m {
			checkItem(i, e)
		}
	}
}

func main() {
	m := generateIntSlice()
	t1 := time.Now()
	llvmSliceReadRange(m)
	t2 := time.Since(t1)
	fmt.Println("llvm slice range time:", t2)
	n := generateIntSlice()
	t3 := time.Now()
	SliceReadRange(n)
	t4 := time.Since(t3)
	fmt.Println("slice range time:", t4)

	fmt.Println("------------")
	t1 = time.Now()
	llvmSliceReadForward(m)
	t2 = time.Since(t1)
	fmt.Println("llvm slice range read forward:", t2)

	t1 = time.Now()
	SliceReadForward(n)
	t2 = time.Since(t1)
	fmt.Println(" slice range read forward:", t2)

	/*
		func llvmSliceFillByIndex() {
		func llvmSliceFillByIndexMake() {
		func llvmSliceFillMakeAppend() {
		func llvmSliceFillAppendNoMake() {
		func llvmSliceFillSmallMakeAppend() {
	*/

	fmt.Println("------------")
	t1 = time.Now()
	llvmSliceReadLastItemFirst(m)
	t2 = time.Since(t1)
	fmt.Println("llvmReadLastItemFirst:", t2)

	t1 = time.Now()
	SliceReadLastItemFirst(m)
	t2 = time.Since(t1)
	fmt.Println("ReadLastItemFirst:", t2)

	fmt.Println("------------")
	t1 = time.Now()
	llvmSliceFillByIndex()
	t2 = time.Since(t1)
	fmt.Println("llvmSliceFillByIndex:", t2)

	t1 = time.Now()
	SliceFillByIndex()
	t2 = time.Since(t1)
	fmt.Println("SliceFillByIndex:", t2)

	/*
		t1 = time.Now()
		llvmSliceFillByIndexMake()
		t2 = time.Since(t1)
		fmt.Println("llvmSliceFillByIndexMake:", t2)
		t1 = time.Now()
		SliceFillByIndexMake()
		t2 = time.Since(t1)
		fmt.Println("SliceFillByIndexMake:", t2)

		fmt.Println("------------")
		t1 = time.Now()
		llvmSliceFillMakeAppend()
		t2 = time.Since(t1)
		fmt.Println("llvmSliceFillMakeAppend:", t2)
		t1 = time.Now()
		SliceFillMakeAppend()
		t2 = time.Since(t1)
		fmt.Println("SliceFillMakeAppend:", t2)

		fmt.Println("------------")
		t1 = time.Now()
		llvmSliceFillAppendNoMake()
		t2 = time.Since(t1)
		fmt.Println("llvmSliceFillAppendNoMake:", t2)
		t1 = time.Now()
		SliceFillAppendNoMake()
		t2 = time.Since(t1)
		fmt.Println("SliceFillAppendNoMake:", t2)

		fmt.Println("------------")
		t1 = time.Now()
		llvmSliceFillSmallMakeAppend()
		t2 = time.Since(t1)
		fmt.Println("llvmSliceFillSmallMakeAppend:", t2)
		t1 = time.Now()
		SliceFillSmallMakeAppend()
		t2 = time.Since(t1)
		fmt.Println("SliceFillSmallMakeAppend:", t2)
	*/
}

//go:noinline
func llvmSliceReadForward(m []int) {
	for n := 0; n < iter; n++ {
		for i := 0; i < BenchMarkSize; i++ {
			e := m[i]
			checkItem(i, e)
		}
	}
}

//go:noinline
func SliceReadForward(m []int) {
	for n := 0; n < iter; n++ {
		for i := 0; i < BenchMarkSize; i++ {
			e := m[i]
			checkItem(i, e)
		}
	}
}

//go:noinline
func llvmSliceReadBackwards(m []int) {
	for n := 0; n < iter; n++ {
		for i := BenchMarkSize - 1; i >= 0; i-- {
			e := m[i]
			checkItem(i, e)
		}
	}
}

//go:noinline
func SliceReadBackwards(m []int) {
	for n := 0; n < iter; n++ {
		for i := BenchMarkSize - 1; i >= 0; i-- {
			e := m[i]
			checkItem(i, e)
		}
	}
}

//go:noinline
func llvmSliceReadLastItemFirst(m []int) {
	for n := 0; n < iter; n++ {
		if m[BenchMarkSize-1] != BenchMarkSize-1 {
			panic("fail")

		}
		for i := 0; i < BenchMarkSize; i++ {
			e := m[i]
			checkItem(i, e)
		}
	}
}

//go:noinline
func SliceReadLastItemFirst(m []int) {
	for n := 0; n < iter; n++ {
		if m[BenchMarkSize-1] != BenchMarkSize-1 {
			panic("fail")

		}
		for i := 0; i < BenchMarkSize; i++ {
			e := m[i]
			checkItem(i, e)
		}
	}
}

//go:noinline
func llvmSliceFillByIndex() {
	for n := 0; n < iter; n++ {
		var m [BenchMarkSize]int
		for i := 0; i < BenchMarkSize; i++ {
			m[i] = i
		}
	}
}

//go:noinline
func SliceFillByIndex() {
	for n := 0; n < iter; n++ {
		var m [BenchMarkSize]int
		for i := 0; i < BenchMarkSize; i++ {
			m[i] = i
		}
	}
}

/*
//go:noinline
func llvmSliceFillByIndexMake() {
	for n := 0; n < iter; n++ {
		m := make([]int, BenchMarkSize)
		for i := 0; i < BenchMarkSize; i++ {
			m[i] = i
		}
	}
}

//go:noinline
func SliceFillByIndexMake() {
	for n := 0; n < iter; n++ {
		m := make([]int, BenchMarkSize)
		for i := 0; i < BenchMarkSize; i++ {
			m[i] = i
		}
	}
}

//go:noinline
func llvmSliceFillMakeAppend() {
	for n := 0; n < iter; n++ {
		m := make([]int, 0, BenchMarkSize)
		for i := 0; i < BenchMarkSize; i++ {
			m = append(m, i)
		}
	}
}
//go:noinline
func SliceFillMakeAppend() {
	for n := 0; n < iter; n++ {
		m := make([]int, 0, BenchMarkSize)
		for i := 0; i < BenchMarkSize; i++ {
			m = append(m, i)
		}
	}
}

/*
//go:noinline
func llvmSliceFillAppendNoMake() {
	for n := 0; n < iter; n++ {
		var m []int

		for i := 0; i < BenchMarkSize; i++ {
			m = append(m, i)
		}
	}
}
//go:noinline
func SliceFillAppendNoMake() {
	for n := 0; n < iter; n++ {
		var m []int

		for i := 0; i < BenchMarkSize; i++ {
			m = append(m, i)
		}
	}
}

//go:noinline
func llvmSliceFillSmallMakeAppend() {
	for n := 0; n < iter; n++ {
		m := make([]int, 0, BenchMarkSize/10)
		for i := 0; i < BenchMarkSize; i++ {
			m = append(m, i)
		}
	}
}

//go:noinline
func SliceFillSmallMakeAppend() {
	for n := 0; n < iter; n++ {
		m := make([]int, 0, BenchMarkSize/10)
		for i := 0; i < BenchMarkSize; i++ {
			m = append(m, i)
		}
	}
}
*/
