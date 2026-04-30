// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conv

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

var r = rand.New(rand.NewSource(99))

func convertToMapJson(from interface{}, c *Converter) (map[string]interface{}, error) {
	var m = make(map[string]interface{})
	jsonContextData, err := json.Marshal(from)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(jsonContextData)))
	dec.UseNumber()
	if err = dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func testRandomMap(t *testing.T, total int, fn func(c *Converter, from interface{}) (map[string]interface{}, error)) {
	if total == 0 {
		total = 1
	}
	initSize := r.Intn(total)
	var from = make(map[string]interface{}, initSize)
	for i := 0; i < total; i++ {
		from[fmt.Sprintf("k%d", i)] = float64(i)
	}

	to, err := fn(&Converter{DeepCopy: true}, from)
	expectEqual(t, err == nil, true, err)
	expectEqual(t, reflect.DeepEqual(from, to), true)
}

func createStringInterfaceMap(total int) map[string]interface{} {
	var m = make(map[string]interface{}, total)
	for i := 0; i < total; i++ {
		m[fmt.Sprintf("k%d", i)] = float64(i)
	}
	return m
}

func testRandomMapMap(t *testing.T, total int, fn func(c *Converter, from interface{}) (map[string]interface{}, error)) {
	if total == 0 {
		total = 1
	}
	initSize := r.Intn(total)
	var from = make(map[string]interface{}, initSize)
	for i := 0; i < total; i++ {
		from[fmt.Sprintf("k%d", i)] = createStringInterfaceMap(i)
	}

	to, err := fn(&Converter{DeepCopy: true}, from)
	expectEqual(t, err == nil, true, err)
	expectEqual(t, reflect.DeepEqual(from, to), true)
}

func TestEmptyStringFast(t *testing.T) {
	a := make(map[string]interface{})
	a["aa"] = json.Number("1")
	a["bb"] = json.Number("2")
	c := &Converter{NumKind: reflect.TypeOf(json.Number(""))}
	checkJSONEqual(t, c, a)
}

func TestEmptyString(t *testing.T) {
	a := make(map[string]interface{})
	a["aa"] = json.Number("1")
	a["bb"] = json.Number("2")
	c := &Converter{NumKind: reflect.TypeOf(json.Number("")), DeepCopy: true}
	checkJSONEqual(t, c, a)
}

func TestRandomMap(t *testing.T) {
	for i := 0; i < 1000; i++ {
		testRandomMap(t, r.Intn(100), (*Converter).ToStringMap)
		testRandomMapMap(t, r.Intn(100), (*Converter).ToStringMap)
	}
}

func BenchmarkRandomMap(t *testing.B) {
	from := createInterfaceMap(100)
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		c := &Converter{DeepCopy: true}
		c.ToStringMap(from)
	}
}

func createInterfaceMap(total int) map[interface{}]interface{} {
	var from = make(map[interface{}]interface{}, total)
	for i := 0; i < total; i++ {
		from[fmt.Sprintf("k%d", i)] = i
	}
	return from
}

var T = 27

func BenchmarkCreateAndConvertToInterfaceMap(b *testing.B) {
	from := createInterfaceMap(T)
	c := &Converter{DeepCopy: true}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.ToStringMap(from)
	}
}

func BenchmarkCreateAndConvertToInterfaceMapDirectly(b *testing.B) {
	from := createInterfaceMap(T)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var to = make(map[interface{}]interface{}, T)
		for k, v := range from {
			to[k] = v
		}
	}
}

func TestPointerCopyOnWrite(t *testing.T) {
	type EnqueueIdReq struct {
		ConfigKey  string  `thrift:"config_key,1" json:"config_key"`
		ObjectId   string  `thrift:"object_id,2" json:"object_id"`
		ObjectType string  `thrift:"object_type,3" json:"object_type"`
		Extra      *string `thrift:"extra,4" json:"extra,omitempty"`
	}
	s1 := EnqueueIdReq{}
	var data = `{"config_key":"hjxtest","extra":"{\"RP_ENQUEUE_SOURCE\":\"pipeline:qa_xunjie\",\"need_bubble_record\":1,\"new_bubble_review\":1}","object_id":"2234","object_type":"item","resp_code":0,"resp_message":"ok"}`
	d1 := json.NewDecoder(strings.NewReader(data))
	err := d1.Decode(&s1)
	expectEqual(t, err == nil, true, err)
	c := &Converter{DeepCopy: false, NumKind: reflect.TypeOf(json.Number(""))}
	s3, err := convertToMapJson(&s1, c)
	expectEqual(t, err == nil, true, err)
	s2, err := c.ToStringMap(s1)
	expectEqual(t, err == nil, true, err)
	s2["extra"] = s1.Extra
	s2, err = c.ToStringMap(s2)
	expectEqual(t, err == nil, true, err)
	expectEqual(t, reflect.DeepEqual(s3, s2), true)
}

func TestJsonNumberCopyOnWrite(t *testing.T) {
	data := `{"Babiana":31.38,"Alkes":"Takelma","propositionally":false}`
	d1 := json.NewDecoder(strings.NewReader(data))
	var m1 map[string]interface{}
	err := d1.Decode(&m1)
	expectEqual(t, err == nil, true, err)
	c := &Converter{DeepCopy: false, NumKind: reflect.TypeOf(json.Number(""))}
	m2, err := c.ToStringMap(m1)
	m3, err := convertToMapJson(&m1, c)
	expectEqual(t, err == nil, true, err)
	expectEqual(t, err == nil, true, err)
	expectEqual(t, reflect.DeepEqual(m2, m3), true)
}

func TestUserConf(t *testing.T) {
	conf := map[interface{}]interface{}{}
	conf["1234"] = "1234"
	c := &Converter{NumKind: reflect.TypeOf(json.Number(""))}
	_, err := c.ToStringMap(&conf)
	expectEqual(t, err == nil, true, err)
}

func TestEmptyMap(t *testing.T) {
	var conf map[interface{}]interface{}
	c := &Converter{NumKind: reflect.TypeOf(json.Number(""))}
	_, err := c.ToStringMap(&conf)
	if err != nil {
		t.Fatal("ConvertStructToMap failed")
	}
}

func TestManyField(t *testing.T) {
	source := `package conv_test

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"reflect"
	"tango/conv"
	"testing"
	"time"
)

type S1 struct {
	%s
}

func FillRandom(s *S1) {
    rand.Seed(time.Now().UnixNano())

    v := reflect.ValueOf(s).Elem()
    for i := 0; i < v.NumField(); i++ {
        f := v.Field(i)
        if f.Kind() == reflect.Int {
            f.SetInt(int64(rand.Intn(101))) // 0~100
        }
    }
}

func TestManyField(t *testing.T) {
	b := S1{}
	FillRandom(&b)
	c := conv.Converter{NumKind: reflect.TypeOf(json.Number(""))}
	d, err := c.ToStringMap(b)
	if err != nil {
		t.Fatal(err)
	}

	be, err := json.Marshal(b)
	if err != nil {
		t.Fatal(be)
	}
	var res map[string]interface{}
	dec := json.NewDecoder(bytes.NewBuffer(be))
	dec.UseNumber()
	err = dec.Decode(&res)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res, d) {
		t.Fatalf("res is %%v\n d is %%v", res, d)
	}
}
`
	sb := strings.Builder{}
	// 3523 is the number of fields that may trigger exceptions.
	for i := 1; i <= 3523; i++ {
		sb.WriteString(fmt.Sprintf("\tF%d int `json:\"f%d,omitempty\"`\n", i, i))
	}
	wd := t.TempDir()
	fn := filepath.Join(wd, "conv_test.go")
	err := os.WriteFile(fn, []byte(fmt.Sprintf(source, sb.String())), 0644)
	expectEqual(t, err == nil, true, err)
	for i := 0; i < 50; i++ {
		cmd := exec.Command(testenv.GoToolPath(t), "test", fn)
		cmd.Dir = wd
		out, err := cmd.CombinedOutput()
		expectEqual(t, err == nil, true, string(out))
	}
}
