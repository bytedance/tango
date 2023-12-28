// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build compilerbeast
// +build compilerbeast

package context

func value(c Context, key any) any {
	switch key.(type) {
	case string:
		return valueString(c, key, key.(string))
	case int:
		return valueInt(c, key, key.(int))
	}
	return valueDefault(c, key)
}

func valueString(c Context, key interface{}, keyS string) interface{} {
	for {
		switch ctx := c.(type) {
		case *valueCtx:
			if k, ok := ctx.key.(string); ok && keyS == k {
				return ctx.val
			}
			c = ctx.Context
		case *cancelCtx:
			c = ctx.Context
		case withoutCancelCtx:
			c = ctx.c
		case *timerCtx:
			c = ctx.Context
		case backgroundCtx, todoCtx:
			return nil
		default:
			return c.Value(key)
		}
	}
}

func valueInt(c Context, key interface{}, keyI int) interface{} {
	for {
		switch ctx := c.(type) {
		case *valueCtx:
			if k, ok := ctx.key.(int); ok && keyI == k {
				return ctx.val
			}
			c = ctx.Context
		case *cancelCtx:
			c = ctx.Context
		case withoutCancelCtx:
			c = ctx.c
		case *timerCtx:
			c = ctx.Context
		case backgroundCtx, todoCtx:
			return nil
		default:
			return c.Value(key)
		}
	}
}
