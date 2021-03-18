// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"unsafe"
)

var rtCfgMap = map[string]unsafe.Pointer{
	"runtime.gab,int32,rw":           unsafe.Pointer(&debug.gab),
	"runtime.gctrace,int32,rw":       unsafe.Pointer(&debug.gctrace),
	"runtime.buildVersion,string,ro": unsafe.Pointer(&buildVersion),
	"runtime.tangoVersion,string,ro": unsafe.Pointer(&tangoVersion),
	"runtime.gcPercent,int32,ro":     unsafe.Pointer(&gcController.gcPercent),
}

//go:linkname rtCfgListAll tango/dynconf.listRuntimeConfigs
func rtCfgListAll() (names []string, addrs []unsafe.Pointer) {
	for name, addr := range rtCfgMap {
		names = append(names, name)
		addrs = append(addrs, addr)
	}
	return
}
