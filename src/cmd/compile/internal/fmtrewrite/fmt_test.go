// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fmtrewrite

import (
	"bytes"
	"fmt"
	"internal/diff"
	"reflect"
	"testing"
)

func tokensToBytes(tokens []token) []byte {
	buf := new(bytes.Buffer)
	for _, token := range tokens {
		switch token.Kind {
		case Literal:
			fmt.Fprintf(buf, "literal: %q\n", token.String)
		case Verb:
			fmt.Fprintf(buf, "verb: %%%c\n", token.Verb)
		}
	}
	return buf.Bytes()
}

func TestParseFormat(t *testing.T) {
	testcases := []struct {
		format   string
		expected []token
	}{
		{
			"%s,%d,%x",
			[]token{verb('s'), literal(","), verb('d'), literal(","), verb('x')},
		},
		{
			"",
			[]token{literal("")},
		},
		{
			"%d%s%d%t",
			[]token{verb('d'), verb('s'), verb('d'), verb('t')},
		},
		{
			"Hello world!",
			[]token{literal("Hello world!")},
		},
		{
			"%%%t",
			[]token{literal("%"), verb('t')},
		},
	}

	for _, testcase := range testcases {
		tokens, ok := parse(testcase.format)
		if !ok {
			t.Errorf("unsupported format %s", testcase.format)
		}
		if !reflect.DeepEqual(tokens, testcase.expected) {
			d := diff.Diff("expect", tokensToBytes(testcase.expected), "got", tokensToBytes(tokens))
			t.Fatalf("wrong parse result: %q\n%s", testcase.format, d)
		}
	}
}
