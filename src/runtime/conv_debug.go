// Copyright 2026 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tangodebug
// +build tangodebug

package runtime

type tcConverter struct {
	numberKind  uint8
	deepCopy    bool
	context     eface
	ssBuffer    tcBuffer[stringStruct]
	bBuffer     tcBuffer[byte]
	replaceFunc uint8
	stackLevel  uint32
	copyTrace   []string
}

func tcGetDebug() bool {
	return true
}

func (c *tcConverter) reportTrace() {
	s := "[TypeToType]: A panic occurred while copying at the following path: "
	for _, sf := range c.copyTrace {
		s += sf + " -> "
	}
	panic(s)
}

func (c *tcConverter) pushTraceByType(t *_type) {
	c.copyTrace = append(c.copyTrace, tcTypeName(t))
}

func (c *tcConverter) pushTraceByString(trace string) {
	c.copyTrace = append(c.copyTrace, trace)
}

func (c *tcConverter) popTrace() {
	c.copyTrace = c.copyTrace[:len(c.copyTrace)-1]
}
