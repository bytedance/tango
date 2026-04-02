// Copyright 2026 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !tangodebug
// +build !tangodebug

package conv

import (
	"internal/testenv"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
)

func TestDebugInfo(t *testing.T) {
	source := `package main

import (
	"reflect"
	"strings"
	"tango/conv"
	"testing"
	"unsafe"
)

func TestDebugInfo(t *testing.T) {
	type S struct {
		Buf []*int
	}

	makeBad := func() S {
		var s S
		hdr := (*reflect.SliceHeader)(unsafe.Pointer(&s.Buf))
		hdr.Data = 1
		hdr.Len = 1000000
		hdr.Cap = 1000000
		return s
	}

	from := makeBad()
	defer func() {
		if err := recover(); err != nil {
			if !strings.Contains(err.(string), "[TypeToType]: A panic occurred while copying at the following path: S -> Buf") {
				t.Fatal(err)
			}
		}
	}()
	var to *S
	err := (&conv.Converter{DeepCopy: true}).TypeToType(&from, &to)
	if err != nil {
		t.Fatal(err)
	}
}
`
	workdir := t.TempDir()
	fileName := "tmp_test.go"
	err := os.WriteFile(path.Join(workdir, fileName), []byte(source), 0666)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(testenv.GoToolPath(t), "test", "-tags", "tangodebug", fileName)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(out))
	}
}

func TestDebugCall(t *testing.T) {
	tags := []string{"tangodebug", ""}
	for _, tag := range tags {
		tagOpt := "-tags=" + tag
		if tag == "" {
			tagOpt = ""
		}
		cmd := exec.Command(testenv.GoToolPath(t), "test", "-c", tagOpt, "-o", "conv.test")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}
		cmd = exec.Command(testenv.GoToolPath(t), "tool", "objdump", "conv.test")
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}
		if strings.Contains(string(out), "conv_debug.go") == (tag == "") {
			t.Fatalf("debug call should not contain")
		}
	}
}
