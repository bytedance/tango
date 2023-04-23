// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynconf

import (
	"fmt"
	r "reflect"
	"sort"
	"strconv"
	"strings"
)

// all known configurations
var allConfigs []*dyncfg

// search for dynamic config with a given name
func getcfg(name string) *dyncfg {
	idx, found := sort.Find(len(allConfigs), func(i int) int {
		return strings.Compare(name, allConfigs[i].name)
	})
	if found {
		return allConfigs[idx]
	}
	return nil
}

// dyncfg defines the interface to operate on a configuration item
type dyncfg struct {
	name string
	rw   bool
	v    r.Value // initialized by reflect.Indirect
}

func (c *dyncfg) String() string {
	perm := "ro"
	if c.rw {
		perm = "rw"
	}
	return fmt.Sprintf("config(%s): %s=%v", perm, c.name, c.v.Interface())
}

func (c *dyncfg) setStringValue(s string) error {
	if !c.rw {
		return fmt.Errorf("config %s is readonly", c.name)
	}
	switch c.v.Kind() {
	case r.Bool:
		if b, err := strconv.ParseBool(s); err == nil {
			c.v.SetBool(b)
		} else {
			return err
		}
	case r.Int8, r.Int16, r.Int32, r.Int64, r.Int:
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			if c.v.OverflowInt(i) {
				return fmt.Errorf("int overflow, skip setting %s=%d", c.name, i)
			}
			c.v.SetInt(i)
		} else {
			return err
		}
	case r.Uint8, r.Uint16, r.Uint32, r.Uint64, r.Uint, r.Uintptr:
		if u, err := strconv.ParseUint(s, 10, 64); err == nil {
			if c.v.OverflowUint(u) {
				return fmt.Errorf("uint overflow, skip setting %s=%d", c.name, u)
			}
			c.v.SetUint(u)
		} else {
			return err
		}
	case r.Float32, r.Float64:
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			if c.v.OverflowFloat(f) {
				return fmt.Errorf("float overflow, skip setting %s=%f", c.name, f)
			}
			c.v.SetFloat(f)
		} else {
			return err
		}
	case r.String:
		c.v.SetString(s)
	default:
		return fmt.Errorf("unsupported dynamic config type %s", c.v.Kind().String())
	}
	return nil
}

// load available configurations from package runtime
func loadRuntimeConfigs() {
	parseConfigs(listRuntimeConfigs())
}

func parseConfigs(cfgs map[string]any) {
	if len(cfgs) == 0 {
		return
	}

	for kn, val := range cfgs {
		ns := strings.Split(kn, ",")
		if len(ns) != 2 {
			panic("unexpected dynconf format: " + kn)
		}
		cfg := &dyncfg{
			name: ns[0],
			rw:   (ns[1] == "rw"),
			v:    r.Indirect(r.ValueOf(val)),
		}
		allConfigs = append(allConfigs, cfg)
	}
}
