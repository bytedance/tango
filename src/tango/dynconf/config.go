// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynconf

import (
	"fmt"
	r "reflect"
	"strconv"
	"strings"
	"unsafe"
)

var (
	// all known configurations
	dyncfgMap = map[string]*dyncfg{}
	// type name to reflect.type
	typeMap = map[string]r.Type{
		"int8":    r.TypeOf(int8(0)),
		"int16":   r.TypeOf(int16(0)),
		"int32":   r.TypeOf(int32(0)),
		"int64":   r.TypeOf(int64(0)),
		"int":     r.TypeOf(int(0)),
		"uint8":   r.TypeOf(uint8(0)),
		"uint16":  r.TypeOf(uint16(0)),
		"uint32":  r.TypeOf(uint32(0)),
		"uint64":  r.TypeOf(uint64(0)),
		"uint":    r.TypeOf(uint(0)),
		"string":  r.TypeOf(""),
		"bool":    r.TypeOf(true),
		"float32": r.TypeOf(float32(0.0)),
		"float64": r.TypeOf(float64(0.0)),
	}
)

// dyncfg defines the interface to operate on a configuration item
type dyncfg struct {
	name string
	rw   bool
	pv   interface{} // previous value
	v    r.Value     // initialized by reflect.NewAt
}

func (c *dyncfg) String() string {
	perm := "ro"
	if c.rw {
		perm = "rw"
	}
	return fmt.Sprintf("config(%s): %s=%v", perm, c.name, c.stringValue())
}

func (c *dyncfg) setStringValue(s string) error {
	if !c.rw {
		return fmt.Errorf("config %s is readonly", c.name)
	}
	pv, e := c.value()
	if e != nil {
		return e
	}
	c.pv = pv
	switch c.v.Kind() {
	case r.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		c.v.SetBool(b)
	case r.Int8, r.Int16, r.Int32, r.Int64, r.Int:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		if !c.v.OverflowInt(i) {
			c.v.SetInt(i)
		} else {
			return fmt.Errorf("int overflow, skip setting %s=%d", c.name, i)
		}
	case r.Uint8, r.Uint16, r.Uint32, r.Uint64, r.Uint:
		u, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		if !c.v.OverflowUint(u) {
			c.v.SetUint(u)
		} else {
			return fmt.Errorf("uint overflow, skip setting %s=%d", c.name, u)
		}
	case r.Float32, r.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		if !c.v.OverflowFloat(f) {
			c.v.SetFloat(f)
		} else {
			return fmt.Errorf("float overflow, skip setting %s=%f", c.name, f)
		}
	case r.String:
		c.v.SetString(s)
	default:
		return fmt.Errorf("unsupported dynamic config type %s", c.v.Kind().String())
	}
	return nil
}

func (c *dyncfg) value() (interface{}, error) {
	switch c.v.Kind() {
	case r.Bool:
		return c.v.Bool(), nil
	case r.Int8, r.Int16, r.Int32, r.Int64, r.Int:
		return c.v.Int(), nil
	case r.Uint8, r.Uint16, r.Uint32, r.Uint64, r.Uint:
		return c.v.Uint(), nil
	case r.Float32, r.Float64:
		return c.v.Float(), nil
	case r.String:
		return c.v.String(), nil
	default:
		return nil, fmt.Errorf("unsupported value of type %s", c.v.Kind().String())
	}
}

func (c *dyncfg) stringValue() string {
	v, e := c.value()
	if e == nil {
		return fmt.Sprintf("%v", v)
	}
	s := fmt.Sprintf("<error: %v>", e)
	fmt.Printf("Failed to get string value of %s, %s\n", c.name, s)
	return s
}

// load available configurations from package runtime
func loadRuntimeConfigs() {
	parseConfigs(listRuntimeConfigs())
}

func parseConfigs(names []string, addrs []unsafe.Pointer) {
	if len(names) != len(addrs) {
		fmt.Printf("dynconf: inconsistent number of names/addrs for package runtime\n")
		return
	}

	for i := 0; i < len(names); i++ {
		ns := strings.Split(names[i], ",")
		t, ok := typeMap[ns[1]]
		if !ok {
			fmt.Printf("dynconf: error parsing type %s for package runtime\n", names[i])
			return
		}
		c := &dyncfg{
			name: ns[0],
			rw:   (ns[2] == "rw"),
			v:    r.Indirect(r.NewAt(t, addrs[i])),
		}
		dyncfgMap[ns[0]] = c
	}
}
