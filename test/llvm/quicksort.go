// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func quickSort(arr []int, low, high int) {
	if low < high {
		var p int
		p = partition(arr, low, high)
		quickSort(arr, low, p-1)
		quickSort(arr, p+1, high)
	}
}

func partition(arr []int, low, high int) int {
	pivot := arr[high]
	i := low
	j := low
	for j < high {
		if arr[j] < pivot {
			arr[i], arr[j] = arr[j], arr[i]
			i = i + 1
		}
		j = j + 1
	}
	arr[i], arr[high] = arr[high], arr[i]
	return i
}

func binarySearch(needle int, haystack []int) bool {
	low := 0
	high := len(haystack) - 1
	for low <= high {
		median := (low + high) / 2
		if haystack[median] < needle {
			low = median + 1
		} else {
			high = median - 1
		}
	}
	if low == len(haystack) || haystack[low] != needle {
		return false
	}
	return true
}

func main() {
	i := 0
	items := []int{90, 30, 100, 70, 40, 20, 63, 80, 15}
	for i < 100000000 {
		i = i + 1
		ii := i % 9
		iii := ii / 2
		items[iii] = items[ii] ^ items[iii]
		items[ii] = items[ii] ^ items[iii]
		items[iii] = items[ii] ^ items[iii]
		quickSort(items, 0, 8)
		if binarySearch(63, items) == false {
			println("wrong")
			break
		}
	}
}
