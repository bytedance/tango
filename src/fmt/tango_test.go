// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fmt_test

import (
	"bytes"
	"fmt"
	"internal/diff"
	"internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"
)

var fmtSprintfTmpl = `package main

import (
	"fmt"
	"time"
)

type I int
func (i I) String() string { return fmt.Sprintf("<%d>", int(i)) }

type E int
func (e E) Error() string { return fmt.Sprintf("errno(%d)", int(e)) }

func main() {
{{range .}}	fmt.Println(fmt.Sprintf("{{.Template}}", {{.Argument}}))
{{end}}}
`

func formatArgument(arg any) (string, bool) {
	if arg == nil {
		return "nil", true
	}
	typ := reflect.TypeOf(arg)
	switch arg := arg.(type) {
	case int, int8, int16, int32, int64, I, E, time.Month:
		typ := strings.TrimPrefix(typ.String(), "fmt_test.")
		return fmt.Sprintf("%v(%v)", typ, reflect.ValueOf(arg).Int()), true
	case uint, uint8, uint16, uint32, uint64:
		val := reflect.ValueOf(arg).Uint()
		return typ.String() + "(" + strconv.FormatUint(val, 10) + ")", true
	case string:
		return strconv.Quote(arg), true
	case bool:
		return strconv.FormatBool(arg), true
	case []byte:
		return "[]byte(" + strconv.Quote(string(arg)) + ")", true
	default:
		// skip test for unknown type
		return "", false
	}

}

func TestSprintfIntrinsic(t *testing.T) {
	type TestCase struct {
		Template string
		Argument string
	}
	var testcases []TestCase

	for _, tc := range fmtTests {
		arg, ok := formatArgument(tc.val)
		if !ok {
			continue
		}
		testcases = append(testcases, TestCase{
			Template: tc.fmt,
			Argument: arg,
		})
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "fmtTest.go")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}

	parse, err := template.New("test").Parse(fmtSprintfTmpl)
	if err != nil {
		t.Fatal(err)
	}
	parse.ExecuteTemplate(file, "test", testcases)

	var beastOutput, stdOutput bytes.Buffer
	cmd := exec.Command(testenv.GoToolPath(t), "run", "-gcflags=all=-beast=fmtrewrite", filePath)
	cmd.Stdout = &beastOutput
	cmd.Stderr = os.Stderr
	err = cmd.Run()

	cmd = exec.Command(testenv.GoToolPath(t), "run", filePath)
	cmd.Stdout = &stdOutput
	cmd.Run()

	if beastOutput.String() != stdOutput.String() {
		d := diff.Diff("std", stdOutput.Bytes(), "beast", beastOutput.Bytes())
		t.Fatalf("beast optimization not compatible: \n%s", d)
	}
}

var sinkString string

func TestSprintfNilAllocation(t *testing.T) {
	var s fmt.Stringer
	var e error
	allocs := testing.AllocsPerRun(200, func() {
		sinkString = fmt.Sprintf("%v%v", s, e)
	})
	if sinkString != "<nil><nil>" {
		t.Errorf("wrong sprintf output: %s", sinkString)
	}
	if allocs != 1 {
		t.Errorf("expected %d allocation, got %d", 1, int(allocs))
	}
}
