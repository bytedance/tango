// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestTangoLLVMEnv(t *testing.T) {
	var runner tester
	flags := flag.NewFlagSet("dist", flag.ContinueOnError)
	flags.Func("env", "", runner.addEnv)
	want := []string{"PATH=/one:/two", "VALUE=a=b c", "EMPTY="}
	args := []string{"-env", want[0], "-env", want[1], "-env", want[2]}
	if err := flags.Parse(args); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runner.env, want) {
		t.Fatalf("env = %q; want %q", runner.env, want)
	}
	for _, value := range []string{"GOGC", "=20", "KEY=bad\x00value"} {
		if err := runner.addEnv(value); err == nil {
			t.Errorf("accepted invalid assignment %q", value)
		}
	}
}
