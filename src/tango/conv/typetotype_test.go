// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conv

import (
	"fmt"
	"internal/testenv"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"
	"unsafe"
)

var defaultConvert = &Converter{}

func expectEqual[T comparable](t *testing.T, from, to T, msg ...any) {
	if !reflect.DeepEqual(to, from) {
		if len(msg) == 0 {
			t.Fatal("Should be equal")
		} else {
			t.Fatal(msg...)
		}
	}
}

func expectNotEqual[T comparable](t *testing.T, from, to T, msg ...any) {
	if reflect.DeepEqual(to, from) {
		if len(msg) == 0 {
			t.Fatal("Should not be equal")
		} else {
			t.Fatal(msg...)
		}
	}
}

func deepCopyWithoutErr(t *testing.T, args ...interface{}) interface{} {
	var from, to interface{}
	switch len(args) {
	case 1:
		from = args[0]
		to = reflect.Zero(reflect.TypeOf(from)).Interface()
	case 2:
		from, to = args[0], args[1]
	}
	err := defaultConvert.TypeToType(from, &to)
	expectEqual(t, err == nil, true, err)
	return to
}

func testStructToStructType(t *testing.T, st reflect.Type) {
	sv := createValue(t, st)
	from := sv.Interface()
	to := deepCopyWithoutErr(t, from)
	expectEqual(t, reflect.DeepEqual(from, to), true, "struct to struct failed")
}

func testStructToStruct(t *testing.T, from interface{}) {
	to := deepCopyWithoutErr(t, from)
	expectEqual(t, reflect.DeepEqual(from, to), true, "struct to struct failed")
}

func TestMultiNestStruct(t *testing.T) {
	type B struct {
		F1 int
	}
	type C struct {
		F2 float64
	}
	type A struct {
		*B
		*C
	}
	testStructToStruct(t, A{&B{1}, &C{2.0}})
}

func TestStructToStructRandom(t *testing.T) {
	r.Seed(testConfig.Seed)
	for i := 0; i < testConfig.TestIteration; i++ {
		st := randomNewStruct()
		testStructToStructType(t, st)
	}
}

func TestDirectMap(t *testing.T) {
	from := map[int]int{1: 2, 2: 3}
	to := deepCopyWithoutErr(t, from)

	expectEqual(t, reflect.DeepEqual(from, to), true)
}

func TestBasePtr(t *testing.T) {
	from := 1
	to := deepCopyWithoutErr(t, from, (*int)(nil)).(*int)

	expectEqual(t, reflect.DeepEqual(from, *to), true)
}

type struct3 struct {
	F1 int
	F2 interface{}
}

func (s struct3) test() {}

func TestEfaceIface(t *testing.T) {
	type tt struct {
		F1 int
		F2 i1
	}
	from := struct3{F1: 1, F2: struct2{1}}
	to := deepCopyWithoutErr(t, from, tt{}).(tt)

	expectEqual(t, from.F1, to.F1)
	expectEqual(t, from.F2.(struct2).F, to.F2.(struct2).F)
}

func TestIfaceEface(t *testing.T) {
	type ft struct {
		F1 int
		F2 i1
	}
	from := ft{F1: 1, F2: struct2{1}}
	to := deepCopyWithoutErr(t, from, struct3{}).(struct3)

	expectEqual(t, from.F1, to.F1)
	expectEqual(t, from.F2.(struct2).F, to.F2.(struct2).F)

	from1 := ft{F1: 1, F2: nil}
	to1 := deepCopyWithoutErr(t, from1, struct3{}).(struct3)

	expectEqual(t, from1.F1, to1.F1)
	expectEqual(t, from1.F2 == to1.F2, true)

	from2 := ft{F1: 1, F2: &struct2{1}}
	to2 := deepCopyWithoutErr(t, from2, struct3{}).(struct3)

	expectEqual(t, from2.F1, to2.F1)
	expectEqual(t, from2.F2.(*struct2).F, to2.F2.(*struct2).F)

	from3 := struct3{F1: 1, F2: nil}
	to3 := deepCopyWithoutErr(t, from3, ft{}).(ft)

	expectEqual(t, from3.F1, to3.F1)
	expectEqual(t, from3.F2 == to3.F2, true)
}

func TestInterfaceMap(t *testing.T) {
	type tt struct {
		F1 int
		F2 map[interface{}]interface{}
	}
	from := struct3{F1: 1, F2: map[int]int{1: 2}}
	to := deepCopyWithoutErr(t, from, tt{}).(tt)

	expectEqual(t, from.F1, to.F1)
	expectEqual(t, from.F2.(map[int]int)[1] == to.F2[1], true)
}

type struct4 struct {
	F directStruct
}

func (s struct4) test() {}

func TestStructInterface(t *testing.T) {
	type ft struct {
		F1 int
		F2 struct2
	}
	type ft1 struct {
		F1 int
		F2 struct4
	}
	type tt struct {
		F1 int
		F2 i1
	}
	from := ft{F1: 1, F2: struct2{1}}
	to := deepCopyWithoutErr(t, from, tt{}).(tt)

	expectEqual(t, from.F1, to.F1)
	expectEqual(t, from.F2.F, to.F2.(struct2).F)

	from1 := ft1{F1: 1, F2: struct4{directStruct{map[int]int{1: 2}}}}
	to1 := deepCopyWithoutErr(t, from1, tt{}).(tt)

	expectEqual(t, from1.F1, to1.F1)
	expectEqual(t, reflect.DeepEqual(from1.F2.F, to1.F2.(struct4).F), true)
}

func TestSliceInterface(t *testing.T) {
	type tt struct {
		F1 int
		F2 i1
	}
	type ft struct {
		F1 int
		F2 []struct2
	}
	from := ft{F1: 1, F2: []struct2{{1}}}
	to := deepCopyWithoutErr(t, from, struct3{}).(struct3)

	expectEqual(t, from.F1, to.F1)
	expectEqual(t, from.F2[0].F, to.F2.([]struct2)[0].F)

	from1 := ft{F1: 1, F2: []struct2{{1}}}
	var to1 tt
	err := defaultConvert.TypeToType(from1, &to1)
	expectNotEqual(t, err == nil, true)

	expectEqual(t, from1.F1, to1.F1)
	expectEqual(t, to1.F2 == nil, true)
}

func TestArrayInterface(t *testing.T) {
	type ft struct {
		F1 int
		F2 [1]struct2
	}
	type tt struct {
		F1 int
		F2 i1
	}
	from := ft{F1: 1, F2: [1]struct2{{1}}}
	to := deepCopyWithoutErr(t, from, struct3{}).(struct3)

	expectEqual(t, from.F1, to.F1)
	expectEqual(t, from.F2[0].F, to.F2.([1]struct2)[0].F)

	from1 := ft{F1: 1, F2: [1]struct2{{1}}}
	var to1 tt
	err := defaultConvert.TypeToType(from1, &to1)
	expectNotEqual(t, err == nil, true)

	expectEqual(t, from1.F1, to1.F1)
	expectEqual(t, to1.F2 == nil, true)
}

func TestConst(t *testing.T) {
	var a = 1
	var b = 2
	b = *deepCopyWithoutErr(t, a, &b).(*int)
	expectEqual(t, b, 1)
}

func TestConstCrash(t *testing.T) {
	c := &Converter{}
	err := c.TypeToType(1, 2)
	expectEqual(t, strings.Contains(err.Error(), "TypeToType: the second argument should be a pointer"), true, err)
}

func TestArrayCrash(t *testing.T) {
	var a = []byte("string")
	s := "000000"
	var b []byte
	slice := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	str := (*reflect.StringHeader)(unsafe.Pointer(&s))
	slice.Data = str.Data
	slice.Len = str.Len
	slice.Cap = str.Len

	b = *deepCopyWithoutErr(t, a, &b).(*[]byte)
	expectEqual(t, string(b), "string")
}

func TestNotCopyZeroValue(t *testing.T) {
	type ft struct {
		Prop  []string `json:"prop,omitempty"`
		Prop1 int      `json:"prop1,omitempty"`
	}

	type tt struct {
		Prop  *[]string `json:"prop,omitempty"`
		Prop1 *int      `json:"prop1,omitempty"`
	}
	from := ft{}
	to := deepCopyWithoutErr(t, from, tt{}).(tt)

	expectEqual(t, to.Prop, nil)
	expectEqual(t, to.Prop1, nil)
}

var fieldList = []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12", "F13"}
var typeList = []string{"*int8", "*int16", "*int32", "*int64", "*int", "*float32", "*float64",
	"*uint8", "*uint16", "*uint32", "*uint64", "*uint", "*uintptr"}
var tpl = `package main

import (
    "math"
    "tango/conv"
    "testing"
)

type Source struct {
%s
}

func getMaxInt8() int8 {
	return math.MaxInt8
}

func getMaxInt16() int16 {
	return math.MaxInt16
}

func getMaxInt32() int32 {
	return math.MaxInt32
}

func getMaxInt64() int64 {
	return math.MaxInt64
}

func getMaxInt() int {
	return math.MaxInt
}

func getMaxUint8() uint8 {
	return math.MaxUint8
}

func getMaxUint16() uint16 {
	return math.MaxUint16
}

func getMaxUint32() uint32 {
	return math.MaxUint32
}

func getMaxUint64() uint64 {
	return math.MaxUint64
}

func getMaxUint() uint {
	return math.MaxUint
}

func getMaxUintptr() uintptr {
	return math.MaxUint64
}

func getMaxFloat32() float32 {
	return math.MaxFloat32
}

func getMaxFloat64() float64 {
	return math.MaxFloat64
}

%s
var from = Source{
%s
}

type Target struct {
%s
}

func TestNumberConvert(t *testing.T) {
  to := Target{}
  c := &conv.Converter{}
  c.TypeToType(&from, &to)
%s
}
`

func generateNumberConvertTest(t *testing.T, fieldNumber int, assignment func(ty string) string) {
	rand.Seed(time.Now().UnixNano())
	l := len(fieldList)
	for i := 0; i < len(typeList); i++ {
		workdir := t.TempDir()
		filename := fmt.Sprintf("%s/number_convert%d_test.go", workdir, i)
		f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0644)
		expectEqual(t, err == nil, true, err)
		source := ""
		for j := 0; j < fieldNumber; j++ {
			source = fmt.Sprintf("%s  %s %s\n", source, fieldList[j], typeList[j])
		}
		sourceNumber := ""
		for j := 0; j < fieldNumber; j++ {
			ty := typeList[j][1:]
			sourceNumber = fmt.Sprintf("%svar n%d %s = %s\n", sourceNumber, j, ty, assignment(ty))
		}
		assign := ""
		for j := 0; j < fieldNumber; j++ {
			assign = fmt.Sprintf("%s    %s: &n%d,\n", assign, fieldList[j], j)
		}
		target := ""
		for j := 0; j < fieldNumber; j++ {
			target = fmt.Sprintf("%s  %s %s\n", target, fieldList[j], typeList[(i+j)%l])
		}
		judge := ""
		for j := 0; j < fieldNumber; j++ {
			ty := typeList[(i+j)%l][1:]
			judge = fmt.Sprintf("%s  if to.%s == nil || *to.%s != %s(n%d) {\n    t.Fatal(\"to conversion failed.\")\n  }\n",
				judge, fieldList[j], fieldList[j], ty, j)
		}
		_, err = fmt.Fprintf(f, tpl, strings.Trim(source, "\n"), sourceNumber, strings.Trim(assign, "\n"),
			strings.Trim(target, "\n"), strings.Trim(judge, "\n"))
		expectEqual(t, err == nil, true, err)
		cmd := exec.Command(testenv.GoToolPath(t), "test", filename)
		cmd.Dir = workdir
		out, err := cmd.CombinedOutput()
		expectEqual(t, err == nil, true, fmt.Sprintf("NumberConvert failed with %s\n", string(out)))
	}
}

func TestNumberConvertBound(t *testing.T) {
	generateNumberConvertTest(t, len(fieldList), func(ty string) string {
		return "getMax" + strings.ToUpper(ty[:1]) + ty[1:] + "()"
	})
}

func TestNumberConvertPositiveNumber(t *testing.T) {
	generateNumberConvertTest(t, len(fieldList), func(ty string) string {
		return strconv.Itoa(rand.Intn(math.MaxInt8))
	})
}

func TestNumberConvertNegativeNumber(t *testing.T) {
	// 7 means the first seven types(int8, int16, int32, int64, int, float32 and float64) of the typeList,
	// because uint8, uint16, uint32, uint64, uint, uintptr don't have negative number.
	generateNumberConvertTest(t, 7, func(ty string) string {
		return strconv.Itoa(-rand.Intn(math.MaxInt8))
	})
}

func TestMultiNestNilStruct(t *testing.T) {
	type C struct {
		F2 float64
	}
	type B struct {
		*C
		F1 int
	}
	type A struct {
		*B
	}
	s3 := A{&B{C: &C{2.0}, F1: 0}}
	testStructToStruct(t, s3)
}

func TestNestedStructWithSameNameField1(t *testing.T) {
	type DDD2 struct {
		EEE int
	}
	type DDD1 struct {
		*DDD2
	}
	type aaa struct {
		EEE *int
		*DDD1
		*DDD2
	}
	c := 1
	bb := aaa{&c, &DDD1{&DDD2{3}}, &DDD2{4}}
	dd := deepCopyWithoutErr(t, bb)
	expectEqual(t, reflect.DeepEqual(bb, dd), true, "TypeToType failed.")
}

func TestNestedStructWithSameNameField2(t *testing.T) {
	type DDD2 struct {
		EEE int
	}
	type DDD1 struct {
		DDD2
	}
	type aaa struct {
		EEE *int
		*DDD1
		*DDD2
	}
	c := 1
	bb := aaa{&c, &DDD1{DDD2{3}}, &DDD2{4}}
	dd := deepCopyWithoutErr(t, bb)
	expectEqual(t, reflect.DeepEqual(bb, dd), true, "TypeToType failed.")
}

func generateNestedStruct(maxNest int) string {
	source := `package main
import (
  "fmt"
  %s
  "tango/conv"
)

type C struct {
    EEE int
}
`
	if maxNest < 32 {
		source = fmt.Sprintf(source, `"reflect"`)
	} else {
		source = fmt.Sprintf(source, "")
	}
	for i := 0; i < maxNest; i++ {
		code := `type C%d struct {
    *C
}
var v%d = &C%d{&C{%d}}

`
		source = source + fmt.Sprintf(code, i, i, i, i)
	}
	code := `type aaa struct {
%s
}
`
	var field string
	for i := 0; i < maxNest; i++ {
		field = field + fmt.Sprintf("  *C%d\n", i)
	}
	source = source + fmt.Sprintf(code, field)
	code = `var v = aaa {
%s
}
`
	field = ""
	for i := 0; i < maxNest; i++ {
		field = field + fmt.Sprintf("  v%d,\n", i)
	}
	source = source + fmt.Sprintf(code, field)
	code = `func main() {
  var dd aaa
  err := (&conv.Converter{}).TypeToType(v, &dd)
  if err != nil {
    fmt.Println(err)
  }
  %s
}
`
	judge := ""
	if maxNest < 32 {
		judge = `if !reflect.DeepEqual(v, dd) {
    fmt.Println("TypeToType failed.")
  }`
	} else {
		for i := 32; i < maxNest; i++ {
			judge = judge + fmt.Sprintf(`if dd.C%d != nil {
  fmt.Println("TypeToType failed.")
  }
`, i)
		}
	}
	source = source + fmt.Sprintf(code, judge)
	return source
}

func TestNestedStructWithSameNameField3(t *testing.T) {
	source := generateNestedStruct(31)
	dir := t.TempDir()
	fileName := path.Join(dir, "nestedstructwithsamenamefield.go")
	err := os.WriteFile(fileName, []byte(source), 0644)
	expectEqual(t, err == nil, true, err)
	cmd := exec.Command(testenv.GoToolPath(t), "run", fileName)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	expectEqual(t, err == nil && string(out) == "", true, fmt.Sprintf("TypeToType failed with %s.", out))
}

func TestNestedStructWithSameNameField4(t *testing.T) {
	source := generateNestedStruct(35)
	dir := t.TempDir()
	fileName := path.Join(dir, "nestedstructwithsamenamefield.go")
	err := os.WriteFile(fileName, []byte(source), 0644)
	expectEqual(t, err == nil, true, err)
	cmd := exec.Command(testenv.GoToolPath(t), "run", fileName)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	expectEqual(t, err == nil, true, fmt.Sprintf("TypeToType failed with %s.", out))
}

func TestNestedStructWithSameNameField5(t *testing.T) {
	type DDD2 struct {
		EEE int
	}
	type DDD1 struct {
		DDD2
	}
	type aaa struct {
		EEE *int
		*DDD1
		*DDD2
	}
	type bbb struct {
		EEE *int
		*DDD1
		*DDD2
	}
	c := 1
	aa := aaa{&c, &DDD1{DDD2{3}}, &DDD2{4}}
	bb := deepCopyWithoutErr(t, aa, bbb{}).(bbb)
	expectEqual(t, *bb.EEE == 1 && bb.DDD1.EEE == 1 && bb.DDD2.EEE == 1, true, "TypeToType failed.")
}

func TestDestNotPointer(t *testing.T) {
	type aaa struct {
		B int
	}
	dd := aaa{}
	err := defaultConvert.TypeToType(aaa{1}, dd)
	expectEqual(t, strings.Contains(err.Error(), "TypeToType: the second argument should be a pointer"), true, err)
}

func TestSpecialJsonTag(t *testing.T) {
	type aaa struct {
		F1 string `json:"-"`
		F2 string `json:"-"`
		F3 string
		F4 string
	}
	type bbb struct {
		F1 string `json:"-"`
		F2 string
		F3 string `json:"-"`
		F4 string
	}
	t.Run("CopyWithSkipUnexportedFieldAndSpecialJsonTag", func(t *testing.T) {
		aa := aaa{"F1", "F2", "F3", "F4"}
		bb := deepCopyWithoutErr(t, aa, bbb{}).(bbb)
		expectEqual(t, bb.F1 == "" && bb.F2 == "" && bb.F3 == "" && bb.F4 == "F4", true, "TypeToType failed.")
	})
	t.Run("CopyWithoutSkipUnexportedFieldAndSpecialJsonTag", func(t *testing.T) {
		aa := aaa{"F1", "F2", "F3", "F4"}
		var bb bbb
		err := (&Converter{DeepCopy: true}).TypeToType(aa, &bb)
		expectEqual(t, err == nil, true, err)
		expectEqual(t, bb.F1 == "F1" && bb.F2 == "F2" && bb.F3 == "F3" && bb.F4 == "F4", true, "TypeToType failed.")
	})
}

func TestEmptyMapWithBuckets(t *testing.T) {
	fm := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		fm[strconv.Itoa(i)] = struct{}{}
	}
	var tm1 map[string]struct{}
	err := defaultConvert.TypeToType(fm, &tm1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		delete(fm, strconv.Itoa(i))
	}
	var tm2 map[string]struct{}
	err = defaultConvert.TypeToType(fm, &tm2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		tm2[strconv.Itoa(i)] = struct{}{}
	}
	if !reflect.DeepEqual(tm1, tm2) {
		t.Fatal("TypeToType failed.")
	}
}

func TestStackOverFlow(t *testing.T) {
	check := func(a interface{}) {
		var b interface{}
		err := defaultConvert.TypeToType(a, &b)
		if err == nil {
			t.Fatal("TypeToType failed.")
		}
		expectEqual(t, strings.Contains(err.Error(), "There may be circular references in the objects to be copied"),
			true, err)
	}
	t.Run("ptr", func(t *testing.T) {
		type A struct {
			F1 *A
		}
		a := &A{}
		a.F1 = a
		check(a)
	})
	t.Run("slice", func(t *testing.T) {
		type A []A
		a := make([]A, 1)
		a[0] = a
		check(a)
	})
	t.Run("map", func(t *testing.T) {
		type A map[string]A
		a := make(map[string]A)
		a["aa"] = a
		check(a)
	})

	const structTpl = `package main
import "tango/conv"
{{range $i := .}}
type Struct{{$i}} struct {
	Struct{{add $i 1}}
	F1 []int
	F2 map[string]string
	F3 *string
}
{{end}}
type Struct1001 struct {
	F1 []int
	F2 map[string]string
	F3 *string
}
func main() {
	s1 := Struct0{}
	str := "aa"
	s1.F1 = make([]int, 1)
	s1.F2 = map[string]string{"aa":"aa"}
	s1.F3 = &str
{{range $i := .}}
	s1.Struct{{add $i 1}}.F1 = make([]int, 1)
	s1.Struct{{add $i 1}}.F2 = map[string]string{"aa":"aa"}
	s1.Struct{{add $i 1}}.F3 = &str
{{end}}
	var s3 any
	err := (&conv.Converter{}).TypeToType(s1, &s3)
	if err != nil {
		panic(err)
	}
}
`
	add := func(a, b int) int {
		return a + b
	}
	tpl, err := template.New("structs").Funcs(template.FuncMap{"add": add}).Parse(structTpl)
	if err != nil {
		panic(err)
	}
	t.Run("1001 nested ptr map slice", func(t *testing.T) {
		// Create a slice of integers from 0 to 1000 for generating struct layers
		layers := make([]int, 1001)
		for i := range layers {
			layers[i] = i
		}
		// Create a file to write the generated code
		dir := t.TempDir()
		t.Logf("gen dir: %s", dir)
		f, err := os.Create(path.Join(dir, "main.go"))
		if err != nil {
			panic(err)
		}
		defer os.Remove(f.Name())
		// Execute the template to generate code and write to the file
		if err := tpl.Execute(f, layers); err != nil {
			panic(err)
		}
		cmd := exec.Command("go", "run", "main.go")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}
	})
}
