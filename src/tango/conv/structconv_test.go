// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conv

import (
	"encoding/json"
	"fmt"
	"internal/testenv"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strings"
	"testing"
)

var (
	basicTypes         []reflect.Type
	intType            = reflect.TypeOf(0)
	emptyInterfaceType = reflect.TypeOf([0]interface{}{}).Elem()
	stringType         = reflect.TypeOf("")
	chars              = " !\"#$%&'()*+,-./0123456789,;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"
)

func init() {
	type Basic struct {
		Bool      bool
		Int       int
		Uint      uint
		Int8      int8
		Uint8     uint8
		Int16     int16
		Uint16    uint16
		Int32     int32
		Uint32    uint32
		Int64     int64
		Uint64    uint64
		Float32   float32
		Float64   float64
		Uintptr   uintptr
		String    string
		Interface interface{}
	}
	ty := reflect.TypeOf(Basic{})
	for i := 0; i < ty.NumField(); i++ {
		basicTypes = append(basicTypes, ty.Field(i).Type)
	}
}

var testConfig = struct {
	Seed              int64
	MaxNumStructField int
	MaxArrayLen       int
	MaxStringLen      int
	MaxSliceLen       int
	MaxMapLen         int
	TestIteration     int
	UsePtrProb        float32
	UseMapProb        float32
	UseSliceProb      float32
	UseArrayProb      float32
}{
	Seed:              2021,
	MaxNumStructField: 100,
	MaxArrayLen:       100,
	MaxStringLen:      100,
	MaxSliceLen:       100,
	MaxMapLen:         100,
	TestIteration:     2000,
	UsePtrProb:        0.2,
	UseMapProb:        0.2,
	UseSliceProb:      0.2,
	UseArrayProb:      0.2,
}

func randomPick(r *rand.Rand, types []reflect.Type) reflect.Type {
	if len(types) == 0 {
		panic("Oops")
	}
	return types[r.Intn(len(types))]
}

func randomChar() byte {
	return chars[r.Intn(len(chars))]
}

func randomNewMapType() reflect.Type {
	k1 := randomPick(r, basicTypes)
	e1 := randomPick(r, basicTypes)
	t1 := reflect.MapOf(safeKeyType(k1), e1)
	return t1
}

func randomNewStruct() reflect.Type {
	var structFields []reflect.StructField

	total := r.Intn(testConfig.MaxNumStructField)

	for i := 0; i < total; i++ {
		t := randomPick(r, basicTypes)
		if r.Float32() < testConfig.UsePtrProb {
			t = reflect.PtrTo(t)
		}
		p := r.Float32()
		if p < testConfig.UseMapProb {
			k := randomPick(r, basicTypes)
			t = reflect.MapOf(safeKeyType(k), safeElemType(t))
		} else if p < testConfig.UseMapProb+testConfig.UseSliceProb {
			if t.Kind() != reflect.Uint8 {
				t = reflect.SliceOf(t)
			}
		} else if p < testConfig.UseMapProb+testConfig.UseSliceProb+testConfig.UseArrayProb {
			t = reflect.ArrayOf(r.Intn(testConfig.MaxArrayLen), t)
		}

		structFields = append(structFields, reflect.StructField{
			Name: fmt.Sprintf("F%d", i),
			Type: t,
		})
	}

	return reflect.StructOf(structFields)
}

func safeKeyType(t reflect.Type) reflect.Type {
	switch t.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return t
	default:
		return stringType
	}
}

func safeElemType(t reflect.Type) reflect.Type {
	return t
}

func createValue(tt *testing.T, t reflect.Type) reflect.Value {
	switch t.Kind() {
	case reflect.Bool:
		if r.Intn(2) == 0 {
			return reflect.ValueOf(true)
		}
		return reflect.ValueOf(false)
	case reflect.Int:
		return reflect.ValueOf(int(r.Uint64()))
	case reflect.Int8:
		return reflect.ValueOf(int8(r.Uint64()))
	case reflect.Int16:
		return reflect.ValueOf(int16(r.Uint64()))
	case reflect.Int32:
		return reflect.ValueOf(int32(r.Uint64()))
	case reflect.Int64:
		return reflect.ValueOf(int64(r.Uint64()))
	case reflect.Uint:
		return reflect.ValueOf(uint(r.Uint64()))
	case reflect.Uint8:
		return reflect.ValueOf(uint8(r.Uint64()))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(r.Uint64()))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(r.Uint64()))
	case reflect.Uint64:
		return reflect.ValueOf(r.Uint64())
	case reflect.Uintptr:
		return reflect.ValueOf(uintptr(r.Uint64()))
	case reflect.Float32:
		return reflect.ValueOf(r.Float32())
	case reflect.Float64:
		return reflect.ValueOf(r.Float64())
	case reflect.Array:
		a := reflect.New(t).Elem()
		for i := 0; i < t.Len(); i++ {
			a.Index(i).Set(createValue(tt, t.Elem()))
		}
		return a
	case reflect.Interface:
		// interface could be any value
		// we use basciTypes here
		if t.NumMethod() != 0 {
			tt.Fatal("Only empty interface is allowed")
		}
		t = randomPick(r, basicTypes)
		if t.Kind() == reflect.Interface {
			t = intType
		}
		it := emptyInterfaceType
		return createValue(tt, t).Convert(it)
	case reflect.Map:
		m := reflect.MakeMap(t)
		total := r.Intn(testConfig.MaxMapLen)
		for i := 0; i < total; i++ {
			k := createValue(tt, t.Key())
			v := createValue(tt, t.Elem())
			m.SetMapIndex(k, v)
		}
		return m
	case reflect.Ptr:
		ev := createValue(tt, t.Elem())
		if ev.Kind() == reflect.Ptr && ev.IsNil() {
			return reflect.ValueOf(nil)
		}
		if !ev.CanAddr() {
			nev := reflect.New(ev.Type())
			nev.Elem().Set(ev)
			return nev
		}
		return ev.Addr()
	case reflect.Slice:
		total := r.Intn(testConfig.MaxSliceLen)
		s := reflect.MakeSlice(t, total, total)

		for i := 0; i < total; i++ {
			v := createValue(tt, t.Elem())
			s.Index(i).Set(v)
		}
		return s
	case reflect.String:
		buf := make([]byte, r.Intn(testConfig.MaxStringLen))
		for i := range buf {
			buf[i] = randomChar()
		}
		return reflect.ValueOf(string(buf))
	case reflect.Struct:
		s := reflect.New(t).Elem()
		for i := 0; i < s.NumField(); i++ {
			f := s.Field(i)
			f.Set(createValue(tt, f.Type()))
		}
		return s
	default:
		tt.Fatal("unknown type: " + t.Name())
	}
	return reflect.Value{}
}

// s is struct or map.
func checkJSONEqual(t *testing.T, c *Converter, s interface{}) {
	m1, err := c.ToStringMap(s)
	expectEqual(t, err == nil, true, err)

	js, err := json.Marshal(s)
	expectEqual(t, err == nil, true, err)
	var m2 map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(string(js)))
	decoder.UseNumber()
	err = decoder.Decode(&m2)
	expectEqual(t, err == nil, true, err)

	expectEqual(t, reflect.DeepEqual(m1, m2), true)
}

func testStructToMapType(t *testing.T, st reflect.Type) {
	sv := createValue(t, st)
	c := &Converter{NumKind: reflect.TypeOf(json.Number("")), DeepCopy: true}
	checkJSONEqual(t, c, sv.Interface())
}

func testStructToMap(t *testing.T, si interface{}) {
	c := &Converter{NumKind: reflect.TypeOf(json.Number("")), DeepCopy: true}
	checkJSONEqual(t, c, si)
}

func testStructToMapFast(t *testing.T, si interface{}) {
	c := &Converter{NumKind: reflect.TypeOf(json.Number(""))}
	checkJSONEqual(t, c, si)
}

type directStruct struct {
	M map[int]int
}

type directArray [1]map[int]int

type struct1 struct {
	F directStruct
}

type i1 interface {
	test()
}

type struct2 struct {
	F int
}

func (s struct2) test() {}

func TestNonemptyInterface(t *testing.T) {
	type st struct {
		F1 int
		F2 i1
	}
	t.Run("ToStringMap", func(t *testing.T) {
		testStructToMap(t, st{F1: 1, F2: &struct2{2}})
		testStructToMap(t, st{F1: 1, F2: nil})
	})
	t.Run("TypeToType", func(t *testing.T) {
		testStructToStruct(t, st{F1: 1, F2: &struct2{2}})
		testStructToStruct(t, st{F1: 1, F2: struct2{2}})
		testStructToStruct(t, st{F1: 1, F2: nil})
	})
}

func TestOmitEmpty(t *testing.T) {
	type st2 struct {
		B  int
		BB float64
	}
	type st1 struct {
		*st2
		A  int
		AA float64
	}
	type st struct {
		*st1
		F  *int `json:"f,omitempty"`
		FF **int
		F3 **int `json:"-"`
		F4 **int `json:"-ast"`
		_  int
		int
	}
	var (
		i  = 0
		ii = &i
	)
	t.Run("ToStringMap1", func(t *testing.T) {
		testStructToMap(t, &st{F: nil})
		testStructToMap(t, st{F: &i})
		testStructToMap(t, st{st1: &st1{AA: 2.384185791015625e-07}, F: &i, FF: &ii, F3: &ii})
		testStructToMap(t, st{F4: &ii, int: 30})
	})
	t.Run("ToStringMap2", func(t *testing.T) {
		a := map[string]*st1{
			"aa": {A: 1, AA: 2.0},
			"bb": {A: 2, AA: 4.0},
		}
		testStructToMap(t, a)
	})
	t.Run("TypeToType", func(t *testing.T) {
		testStructToStruct(t, st{F: nil})
		testStructToStruct(t, st{F: &i})
		testStructToStruct(t, st{st1: &st1{&st2{1, 1.0}, 1, 1.0}, F: &i, FF: &ii})
	})
}

func TestDirectInterface(t *testing.T) {
	r.Seed(testConfig.Seed)
	type st1 struct {
		F directArray
	}
	type st2 struct {
		M map[int]int
	}
	type st3 struct {
		F interface{}
	}
	var (
		rt1 = reflect.TypeOf(struct1{})
		rt2 = reflect.TypeOf(st1{})
		rt3 = reflect.TypeOf(st2{})
	)

	t.Run("ToStringMap", func(t *testing.T) {
		for i := 0; i < testConfig.TestIteration; i++ {
			testStructToMapType(t, rt1)
			testStructToMapType(t, rt2)
		}

		testStructToMap(t, &st3{})
		testStructToMap(t, st3{F: nil})
		testStructToMap(t, st3{F: createValue(t, rt1).Interface()})
		testStructToMap(t, st3{F: createValue(t, rt2).Interface()})
		testStructToMap(t, st3{F: createValue(t, rt3).Interface()})
	})
	t.Run("TypeToType", func(t *testing.T) {
		for i := 0; i < testConfig.TestIteration; i++ {
			testStructToStructType(t, rt1)
			testStructToStructType(t, rt2)
		}

		testStructToStruct(t, &st3{})
		testStructToStruct(t, st3{F: nil})
		testStructToStruct(t, st3{F: createValue(t, rt1).Interface()})
		testStructToStruct(t, st3{F: createValue(t, rt2).Interface()})
		testStructToStruct(t, st3{F: createValue(t, rt3).Interface()})
	})
}

func TestZeroLengthSlice(t *testing.T) {
	type st struct {
		A []struct1
	}
	t.Run("ToStringMap", func(t *testing.T) {
		testStructToMap(t, st{[]struct1{}})
	})
	t.Run("TypeToType", func(t *testing.T) {
		testStructToStruct(t, st{[]struct1{}})
	})
}

func TestEmptyStruct(t *testing.T) {
	type st struct{}
	testStructToMap(t, st{})
}

func TestNilMapInStruct(t *testing.T) {
	type st struct {
		A int
		M map[string]interface{}
	}
	type st1 struct {
		A int `json:"-"`
		M *map[int]int
	}
	t.Run("ToStringMap", func(t *testing.T) {
		testStructToMapFast(t, st{1, nil})
	})
	t.Run("TypeToType", func(t *testing.T) {
		testStructToStruct(t, st{1, nil})

		m := map[int]int{1: 1, 2: 2}
		c := &Converter{DeepCopy: true}
		ts := st1{}
		fs := st1{1, &m}
		err := c.TypeToType(&fs, &ts)
		expectEqual(t, err == nil, true, err)
		expectEqual(t, reflect.DeepEqual(fs, ts), true)
	})
}

func TestStructToMap(t *testing.T) {
	r.Seed(testConfig.Seed)
	for i := 0; i < testConfig.TestIteration; i++ {
		st := randomNewStruct()
		testStructToMapType(t, st)
	}
}

func testMapToMap(t *testing.T, mt reflect.Type) {
	src := createValue(t, mt)
	c := &Converter{NumKind: reflect.TypeOf(json.Number("")), DeepCopy: true}
	checkJSONEqual(t, c, src)
}

func TestMapToMap(t *testing.T) {
	r.Seed(testConfig.Seed)
	for i := 0; i < testConfig.TestIteration; i++ {
		mt := randomNewMapType()
		testMapToMap(t, mt)
		testMapToMap(t, mt)
	}
}

func TestUnexportField(t *testing.T) {
	ss := struct {
		F1 int
		f2 string
		F3 map[string]interface{}
	}{F1: 1, f2: "f2", F3: map[string]interface{}{"k1": "e1", "k2": "e2"}}
	c := &Converter{NumKind: reflect.TypeOf(json.Number(""))}
	checkJSONEqual(t, c, ss)
}

func TestCustomTypeWithDefaultConfig(t *testing.T) {
	typs := [...]string{"int8", "int16", "int32", "int64", "int",
		"uint8", "uint16", "uint32", "uint64", "uint", "uintptr"}
	source := `package main
import (
  "testing"
  "tango/conv"
)

func TestCustomTypeWithDefaultConfig(t *testing.T) {
  type enum %s
  type aaa struct {
    F1 enum
  }
  s1 := aaa{30}
  c := &conv.Converter{}
  m, err := c.ToStringMap(s1)
  if err != nil {
    t.Fatal(err)
  }
  if m["F1"].(%s) != 30 {
    t.Fatal("ToStringMap not equal when type is %s.")
  }
}
`
	for i := range typs {
		dir := t.TempDir()
		fileName := path.Join(dir, fmt.Sprintf("%s_test.go", typs[i]))
		err := os.WriteFile(fileName, []byte(fmt.Sprintf(source, typs[i], typs[i], typs[i])), 0644)
		expectEqual(t, err == nil, true, err)
		cmd := exec.Command(testenv.GoToolPath(t), "test", fileName)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		expectEqual(t, err == nil && !strings.Contains(string(out), "ToStringMap not equal."),
			true, fmt.Sprintf("ToStringMap failed with %s", out))
	}
}

func TestFieldDelete(t *testing.T) {
	type s1 struct {
		F1  string      `json:"f1,omitempty"`
		F2  string      `json:"f2,omitempty"`
		F3  bool        `json:"f3,omitempty"`
		F4  string      `json:"f4,omitempty"`
		F5  string      `json:"f5,omitempty"`
		F6  string      `json:"f6,omitempty"`
		F7  string      `json:"f7,omitempty"`
		F8  interface{} `json:"f8,omitempty"`
		F9  string      `json:"f9,omitempty"`
		F10 bool        `json:"f10,omitempty"`
		F11 string      `json:"f11,omitempty"`
		F12 string      `json:"f12,omitempty"`
		F13 bool        `json:"f13,omitempty"`
		F14 bool        `json:"f14,omitempty"`
		F15 int         `json:"f15,omitempty"`
		F16 interface{} `json:"f16,omitempty"`
		F17 string      `json:"f17,omitempty"`
		F18 int         `json:"f18,omitempty"`
		F19 int         `json:"f19,omitempty"`
		F20 bool        `json:"f20,omitempty"`
		F21 int         `json:"f21,omitempty"`
		F22 string      `json:"f22,omitempty"`
		F23 string      `json:"f23,omitempty"`
		F24 int         `json:"f24,omitempty"`
		F25 bool        `json:"f25,omitempty"`
		F26 interface{} `json:"f26,omitempty"`
		F27 string      `json:"f27,omitempty"`
		F28 string      `json:"f28,omitempty"`
		F29 bool        `json:"f29,omitempty"`
		F30 bool        `json:"f30,omitempty"`
		F31 string      `json:"f31,omitempty"`
		F32 string      `json:"f32,omitempty"`
		F33 bool        `json:"f33,omitempty"`
		F34 string      `json:"f34,omitempty"`
		F35 int         `json:"f35,omitempty"`
		F36 int         `json:"f36,omitempty"`
		F37 interface{} `json:"f37,omitempty"`
		F38 bool        `json:"f38,omitempty"`
		F39 string      `json:"f39,omitempty"`
	}
	data := `{
        "f1": "DDFFCCF",
        "f2": "BDDCADF",
        "f3": true,
        "f4": "Chaz Martin",
        "f5": "https://ccc.twimg.com/rrr/447958234/Ijsua.jpg",
        "f6": "Tue Apr 07 20:05:07 +0000 2009",
        "f7": "Durham, NPC",
        "f8": null,
        "f9": "00842B4",
        "f10": false,
        "f11": "295162338",
        "f12": "abcef",
        "f13": false,
        "f14": false,
        "f15": 9,
        "f16": "https://aaa.com/wnng/",
        "f17": "https://rfd.twimg.com/rgd/447958234/sawqD.jpg",
        "f18": -180000,
        "f19": 295162238,
        "f20": true,
        "f21": 1138,
        "f22": "3333333",
        "f23": "zh",
        "f24": 20522,
        "f25": false,
        "f26": null,
        "f27": "https://sa.sd.com/ef/9423277/asx.bmp",
        "f28": "9AE4E822",
        "f29": false,
        "f30": false,
        "f31": "ABC Time (CN & Canada)",
        "f32": "You will come to SA, S A. I will sell you some records then, here in Edsa,Wsdaf Esasdf. Fun will happen.",
        "f33": false,
        "f34": "https://asd.wa.com/fgr/9423277/svbh.bmp",
        "f35": 75719,
        "f36": 3428,
        "f37": null,
        "f38": true,
        "f39": "asafefsft"}`
	var s s1
	err := json.Unmarshal([]byte(data), &s)
	expectEqual(t, err == nil, true, err)
	c := &Converter{DeepCopy: true, NumKind: reflect.TypeOf(json.Number(""))}
	m1, err := c.ToStringMap(s)
	expectEqual(t, err == nil, true, err)
	m2, err := convertToMapJson(s, defaultConvert)
	expectEqual(t, err == nil, true, err)
	expectEqual(t, reflect.DeepEqual(m1, m2), true)
}

func BenchmarkStruct(b *testing.B) {
	type Author struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
		Male bool   `json:"male"`
	}

	type Book struct {
		BookId  int32     `json:"id"`
		BookIds []int     `json:"ids"`
		Title   string    `json:"title"`
		Titles  []string  `json:"titles"`
		Price   float64   `json:"price"`
		Prices  []float64 `json:"prices"`
		Hot     bool      `json:"hot"`
		Hots    []bool    `json:"hots"`
		Author  Author    `json:"author"`
		Authors []Author  `json:"authors"`
		Weights []int     `json:"weights"`
	}

	var AuthorIns = Author{
		Name: "john",
		Age:  56,
		Male: true,
	}

	var BookIns = Book{
		BookId:  11111111,
		BookIds: []int{-2147483648, 2147483647},
		Title:   "Theory of Compilation",
		Titles:  []string{"hello", "world"},
		Price:   40.9,
		Prices:  []float64{-0.2, 0.6},
		Hot:     true,
		Hots:    []bool{true, true, false},
		Author:  AuthorIns,
		Authors: []Author{AuthorIns, AuthorIns},
		Weights: nil,
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c := &Converter{NumKind: reflect.TypeOf(json.Number("")), DeepCopy: true}
			c.ToStringMap(BookIns)
		}
	})
}
