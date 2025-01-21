// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

func fmt_bool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func fmt_itoa(buf *[24]byte, v int64) string {
	var neg bool
	if v < 0 {
		neg = true
		v = -v
	}
	i := fmt_douitoa(buf, uint64(v))
	if neg {
		i--
		buf[i] = '-'
	}
	return unsafe.String(&buf[i], 24-i)
}

// fmt_uitoa converts val to a decimal string.
func fmt_uitoa(buf *[24]byte, v uint64) string {
	i := fmt_douitoa(buf, v)
	return unsafe.String(&buf[i], 24-i)
}

// fmt_douitoa converts val to a decimal string.
func fmt_douitoa(buf *[24]byte, v uint64) int {
	i := len(buf) - 1
	for v >= 10 {
		q := v / 10
		buf[i] = byte('0' + v%10)
		i--
		v = q
	}
	// val < 10
	buf[i] = byte('0' + v)
	return i
}

func fmt_hex(buf *[24]byte, v int64) string {
	var neg bool
	if v < 0 {
		neg = true
		v = -v
	}
	i := fmt_douhex(buf, uint64(v))
	if neg {
		i--
		buf[i] = '-'
	}
	return unsafe.String(&buf[i], 24-i)
}

func fmt_uhex(buf *[24]byte, v uint64) string {
	i := fmt_douhex(buf, v)
	return unsafe.String(&buf[i], 24-i)
}

func fmt_douhex(buf *[24]byte, v uint64) int {
	const digits = "0123456789abcdef"
	i := len(buf)
	for i--; i > 0; i-- {
		buf[i] = digits[v%16]
		if v < 16 {
			break
		}
		v /= 16
	}
	return i
}

func fmt_stringcopy(s1 string) string {
	if len(s1) == 0 {
		return ""
	}
	s2, b := rawstring(len(s1))
	copy(b, s1)
	return s2
}

var fmtSprint func(...any) string

//go:linkname initFmtSprint
func initFmtSprint(f func(...any) string) {
	fmtSprint = f
}

func fmt_stringer(x stringer) (s string) {
	if x == nil {
		return "<nil>"
	}
	defer func() {
		if err := recover(); err != nil {
			s = fmtSprint(x)
		}
	}()
	return x.String()
}

func fmt_error(x error) (s string) {
	if x == nil {
		return "<nil>"
	}
	defer func() {
		if err := recover(); err != nil {
			s = fmtSprint(x)
		}
	}()
	return x.Error()
}
