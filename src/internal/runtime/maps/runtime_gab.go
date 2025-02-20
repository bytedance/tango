// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package maps

import (
	"unsafe"
)

//go:linkname getSwissMapGroupPtrs runtime.getSwissMapGroupPtrs
func getSwissMapGroupPtrs(m unsafe.Pointer) []unsafe.Pointer {
	p := *(**Map)(m)
	if p.dirLen == 0 {
		return []unsafe.Pointer{p.dirPtr}
	}

	groupPtrs := make([]unsafe.Pointer, 0, p.dirLen)
	for i := range p.dirLen {
		t := p.directoryAt(uintptr(i))
		groupPtrs = append(groupPtrs, t.groups.data)
	}

	return groupPtrs
}
