// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/runtime/atomic"
	"unsafe"
)

var (
	modulesSliceSorted   *[]*moduledata
	pluginSliceSorted    *[]*moduledata
	lastUpdateModuledata *moduledata = nil
)

var (
	needUpdated          atomic.Int32
	moduleDataSearchHit  atomic.Uint64
	moduleDataSearchMiss atomic.Uint64
)

//go:noinline
func checkUpdateSortedModuleData() {
	assertWorldStopped()
	// If modulesSliceSorted has not yet been initialized, set needUpdated to -1.
	// Otherwise, set needUpdated to the count of newly added modules.
	var count int32 = -1
	if lastUpdateModuledata != nil {
		count = 0
		// just calculate count, avoid memory alloc during STW.
		for head := lastUpdateModuledata.next; head != nil; head = head.next {
			count++
		}
	}
	if debug.moduledatadebug > 0 {
		println("[ModuleDebug]", "checkUpdateSortedModuleData:", count)
		println("[ModuleDebug]", "moduleDataSearchHit:", moduleDataSearchHit.Load())
		println("[ModuleDebug]", "moduleDataSearchMiss:", moduleDataSearchMiss.Load())
	}
	needUpdated.Store(count)
}

//go:noinline
func updateSortedModuleData() {
	count := needUpdated.Load()
	if count == 0 {
		return
	}
	modules := new([]*moduledata)
	plugins := new([]*moduledata)
	if debug.moduledatadebug > 0 {
		println("[ModuleDebug]", "updateSortedModuleData", count)
	}
	if count < 0 {
		// Build modulesSliceSorted for the first time, traverse all moduledatas.
		for md := &firstmoduledata; md != nil; md = md.next {
			lastUpdateModuledata = md
			// Skip bad moduledata.
			if md.bad {
				continue
			}
			if debug.moduledatadebug > 0 {
				println("[ModuleDebug]", "add to modulesSliceSorted", md.minpc)
			}
			*modules = append(*modules, md)
			if md.pluginpath != "" || md.hasmain == 1 {
				*plugins = append(*plugins, md)
				if debug.moduledatadebug > 0 {
					println("[ModuleDebug]", "add to pluginSliceSorted", md.minpc)
				}
			}
		}
		// Sort new added moduledata.
		quickSort(*modules)
		if len(*plugins) > 0 {
			quickSort(*plugins)
		}
	}
	if count > 0 {
		// Only add the new needUpdated moduledatas.
		if lastUpdateModuledata == nil || lastUpdateModuledata.next == nil {
			needUpdated.Store(0)
			return
		}
		newMods := make([]*moduledata, 0, count)
		newPlugins := make([]*moduledata, 0, 5)
		md := lastUpdateModuledata.next
		for i := int32(0); i < count && md != nil; i++ {
			// Skip bad moduledata.
			if md.bad {
				continue
			}
			newMods = append(newMods, md)
			if debug.moduledatadebug > 0 {
				println("[ModuleDebug]", "add to modulesSliceSorted", md.minpc)
			}
			if md.pluginpath != "" {
				newPlugins = append(newPlugins, md)
				if debug.moduledatadebug > 0 {
					println("[ModuleDebug]", "add to pluginSliceSorted", md.minpc)
				}
			}
			lastUpdateModuledata = md
			md = md.next
		}
		// Sort new added moduledata.
		if len(newMods) > 1 {
			quickSort(newMods)
		}
		if len(newPlugins) > 1 {
			quickSort(newPlugins)
		}
		// Two sorted slice, merge sort.
		oldModule := *(*[]*moduledata)(atomic.Loadp(unsafe.Pointer(&modulesSliceSorted)))
		modules = mergeSortedModuleData(oldModule, newMods)
		if len(newPlugins) > 0 {
			oldPlugins := *(*[]*moduledata)(atomic.Loadp(unsafe.Pointer(&pluginSliceSorted)))
			plugins = mergeSortedModuleData(oldPlugins, newPlugins)
		}
	}
	if debug.moduledatadebug > 0 {
		println("[ModuleDebug]", "updateSortedModuleData len", len(*modules))
	}
	// Clear flag.
	needUpdated.Store(0)
	// Release new modulesSliceSorted.
	atomicstorep(unsafe.Pointer(&modulesSliceSorted), unsafe.Pointer(modules))
	if len(*plugins) > 0 {
		atomicstorep(unsafe.Pointer(&pluginSliceSorted), unsafe.Pointer(plugins))
	}
	if debug.moduledatadebug > 1 {
		dumpAndCheckModulesSlice()
	}
}

func mergeSortedModuleData(md1 []*moduledata, md2 []*moduledata) *[]*moduledata {
	result := new([]*moduledata)
	*result = make([]*moduledata, 0, len(md1)+len(md2))
	i, j := 0, 0
	for i < len(md1) && j < len(md2) {
		if md1[i].minpc <= md2[j].minpc {
			*result = append(*result, md1[i])
			i++
		} else {
			*result = append(*result, md2[j])
			j++
		}
	}
	*result = append(*result, md1[i:]...)
	*result = append(*result, md2[j:]...)
	return result
}

// It is nosplit because it's part of the findmoduledatap implementation.
//
//go:nosplit
//go:noinline
func findModuledataByPc(pc uintptr) *moduledata {
	// fast path, check firstmoduledata.
	if firstmoduledata.minpc <= pc && pc < firstmoduledata.maxpc {
		if debug.moduledatadebug > 0 {
			moduleDataSearchHit.Add(1)
		}
		return &firstmoduledata
	}
	mss := atomic.Loadp(unsafe.Pointer(&modulesSliceSorted))
	if mss == nil {
		if debug.moduledatadebug > 0 {
			moduleDataSearchMiss.Add(1)
		}
		return nil
	}
	smd := *(*[]*moduledata)(mss)
	if len(smd) == 0 || pc < smd[0].minpc || pc >= smd[len(smd)-1].maxpc {
		if debug.moduledatadebug > 0 {
			moduleDataSearchMiss.Add(1)
		}
		return nil
	}
	left, right := 0, len(smd)-1
	for left <= right {
		mid := left + (right-left)>>1
		current := smd[mid]
		if pc < current.minpc {
			right = mid - 1
		} else if pc >= current.maxpc {
			left = mid + 1
		} else {
			if debug.moduledatadebug > 0 {
				moduleDataSearchHit.Add(1)
			}
			return current
		}
	}
	if debug.moduledatadebug > 0 {
		moduleDataSearchMiss.Add(1)
	}
	return nil
}

// The search logic of findModuledataByType and findModuledataByPc is similar.
// When extracting the common part and using min and max as function type variables,
// they cannot be fully inlined, which will introduce additional consumption.
//
//go:noinline
func findModuledataByType(typ uintptr) *moduledata {
	// fast path, check firstmoduledata.
	if firstmoduledata.types <= typ && typ < firstmoduledata.etypes {
		if debug.moduledatadebug > 0 {
			moduleDataSearchHit.Add(1)
		}
		return &firstmoduledata
	}
	mss := atomic.Loadp(unsafe.Pointer(&pluginSliceSorted))
	if mss == nil {
		if debug.moduledatadebug > 0 {
			moduleDataSearchMiss.Add(1)
		}
		return nil
	}
	smd := *(*[]*moduledata)(mss)
	if len(smd) == 0 || typ < smd[0].types || typ >= smd[len(smd)-1].etypes {
		if debug.moduledatadebug > 0 {
			moduleDataSearchMiss.Add(1)
		}
		return nil
	}
	left, right := 0, len(smd)-1
	for left <= right {
		mid := left + (right-left)>>1
		current := smd[mid]
		if typ < current.types {
			right = mid - 1
		} else if typ >= current.etypes {
			left = mid + 1
		} else {
			if debug.moduledatadebug > 0 {
				moduleDataSearchHit.Add(1)
			}
			return current
		}
	}
	if debug.moduledatadebug > 0 {
		moduleDataSearchMiss.Add(1)
	}
	return nil
}

//go:noinline
func findModuledataByGCdata(typ uintptr) *moduledata {
	// fast path, check firstmoduledata.
	if firstmoduledata.gofunc <= typ && typ < firstmoduledata.end {
		if debug.moduledatadebug > 0 {
			moduleDataSearchHit.Add(1)
		}
		return &firstmoduledata
	}
	mss := atomic.Loadp(unsafe.Pointer(&pluginSliceSorted))
	if mss == nil {
		if debug.moduledatadebug > 0 {
			moduleDataSearchMiss.Add(1)
		}
		return nil
	}
	smd := *(*[]*moduledata)(mss)
	if len(smd) == 0 || typ < smd[0].gofunc || typ >= smd[len(smd)-1].end {
		if debug.moduledatadebug > 0 {
			moduleDataSearchMiss.Add(1)
		}
		return nil
	}
	left, right := 0, len(smd)-1
	for left <= right {
		mid := left + (right-left)>>1
		current := smd[mid]
		if typ < current.gofunc {
			right = mid - 1
		} else if typ >= current.end {
			left = mid + 1
		} else {
			if debug.moduledatadebug > 0 {
				moduleDataSearchHit.Add(1)
			}
			return current
		}
	}
	if debug.moduledatadebug > 0 {
		moduleDataSearchMiss.Add(1)
	}
	return nil
}

func quickSort(md []*moduledata) {
	if len(md) <= 1 {
		return
	}
	pivot := md[0]
	left, right := 0, len(md)-1

	for left < right {
		for left < right && md[right].minpc >= pivot.minpc {
			right--
		}
		md[left] = md[right]

		for left < right && md[left].minpc <= pivot.minpc {
			left++
		}
		md[right] = md[left]
	}
	md[left] = pivot

	quickSort(md[:left])
	quickSort(md[left+1:])
}

func dumpAndCheckModulesSlice() {
	println("[ModuleDebug]", "activeModules")
	mds := activeModules()
	for i, v := range mds {
		println("[ModuleDebug]", i, ":", v.minpc, "~", v.maxpc, " ", v.types, "~", v.etypes, " ", v.gofunc, "~", v.end)
	}
	println("[ModuleDebug]", "modulesSliceSorted")
	mss := atomic.Loadp(unsafe.Pointer(&modulesSliceSorted))
	if mss == nil {
		return
	}
	smd := *(*[]*moduledata)(mss)
	var prev *moduledata
	for i, v := range smd {
		println("[ModuleDebug]", i, ":", v.minpc, "~", v.maxpc, " ", v.types, "~", v.etypes, " ", v.gofunc, "~", v.end, " ", v.hasmain == 1)
		if prev == nil {
			prev = v
		} else {
			if v.minpc < prev.maxpc {
				println("[ModuleDebug]", "error pc not sorted")
			}
			prev = v
		}
	}

	println("[ModuleDebug]", "pluginSliceSorted")
	mss = atomic.Loadp(unsafe.Pointer(&pluginSliceSorted))
	if mss == nil {
		return
	}
	smd = *(*[]*moduledata)(mss)
	prev = nil
	for i, v := range smd {
		println("[ModuleDebug]", i, ":", v.minpc, "~", v.maxpc, " ", v.types, "~", v.etypes, " ", v.gofunc, "~", v.end, " ", v.pluginpath)
		if prev == nil {
			prev = v
		} else {
			if v.minpc < prev.maxpc {
				println("[ModuleDebug]", "error pc not sorted")
			}
			if v.types < prev.etypes {
				println("[ModuleDebug]", "error type not sorted")
			}
			if v.gofunc < prev.end {
				println("[ModuleDebug]", "error gofunc not sorted")
			}
			prev = v
		}
	}
	println("[ModuleDebug]", "done")
}

func addPluginDebug(md *moduledata) {
	println("[ModuleDebug]", "add plugin", md.pluginpath, md.minpc, md.maxpc)
}
