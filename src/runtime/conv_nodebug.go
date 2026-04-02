// Copyright 2026 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !tangodebug
// +build !tangodebug

package runtime

type tcConverter struct {
	numberKind  uint8
	deepCopy    bool
	context     eface
	ssBuffer    tcBuffer[stringStruct]
	bBuffer     tcBuffer[byte]
	replaceFunc uint8
	stackLevel  uint32
}

func tcGetDebug() bool {
	return false
}

func (c *tcConverter) reportTrace() {}

func (c *tcConverter) pushTraceByType(t *_type) {}

func (c *tcConverter) pushTraceByString(trace string) {}

func (c *tcConverter) popTrace() {}
