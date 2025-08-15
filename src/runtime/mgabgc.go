// Copyright 2014 The Go Authors. All rights reserved.
// Copyright (c) 2021 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2021-10-25.

package runtime

import (
	"internal/abi"
	"internal/goarch"
	"internal/goexperiment"
	"internal/runtime/atomic"
	"internal/runtime/gc"
	"internal/runtime/math"
	"internal/runtime/sys"
	"unsafe"
)

var (
	sgab            *stwgab
	gabIsEvacuating bool
)

//go:nowritebarrierrec
func (s *mspan) isGab() bool {
	return s.spanclass >= numNonGabSpanClasses
}

//go:nowritebarrierrec
func (s *mspan) doEvacuation() bool {
	return gabState.isEvacuation && s.isGab()
}

//go:nowritebarrierrec
//go:linkname isEvacEnabled
func isEvacEnabled() bool {
	return debug.gab == 2
}

//go:linkname mapsIsEvacEnabled internal/runtime/maps.isEvacEnabled
func mapsIsEvacEnabled() bool {
	return isEvacEnabled()
}

//go:nowritebarrierrec
func (s *mspan) gabPinBitsForIndex(objIndex uintptr) markBits {
	bytep, mask := s.gabPinBits.bitp(objIndex)
	return markBits{bytep, mask, objIndex}
}

//go:nowritebarrierrec
func (s *mspan) isPinnedGab(p uintptr) bool {
	if !s.isGab() || p < s.base() || p > s.limit || s.gabPinBits == nil {
		throw("Error GAB mspan")
	}
	pinBits := s.gabPinBitsForIndex(s.objIndex(p))
	return pinBits.isMarked()
}

//go:nowritebarrierrec
func pinIfOnHeap(ptr uintptr) {
	s := spanOfHeap(ptr)
	if s == nil {
		return
	}
	if s.isGab() && !s.isFree(s.objIndex(ptr)) {
		s.gabPinObj(ptr)
	}
}

// Pin GAB-allocated objects.
// All the objects in the same GAB are not copied in this GC cycle.
// All the pin bits are cleared at the end of GAB evacuation GC cycle.
//
//go:nowritebarrierrec
func (s *mspan) gabPinObj(obj uintptr) {
	index := s.objIndex(obj)
	pinBits := s.gabPinBitsForIndex(index)
	pinBits.setMarked()
}

// Entry of GAB evacuation
//
//go:nowritebarrierrec
func gabEvacuate() {
	assertWorldStopped()
	if debug.gab < 2 {
		throw("GAB evacuation must have been enabled")
	}
	if writeBarrier.enabled {
		throw("Write barrier should have been disabled")
	}
	gabgcstats.balancedGCStartNs = nanotime()
	checkAllG()
	gabgcstats.evacStartNs = nanotime()
	gabIsEvacuating = true

	for _, pp := range allp {
		pp.refsIn.refsDo(doRef)
	}

	gabgcstats.evacEndNs = nanotime()
	gabIsEvacuating = false
	gabgcstats.balancedGCEndNs = nanotime()
	gabState.updateGabEvacuated(uint64(gabgcstats.gabEvacuatedBytes))
}

//go:nowritebarrierrec
func doRef(addr uintptr) bool {
	// Examine the validity of references
	fromSpan := spanOf(addr)
	if fromSpan == nil {
		atomic.Xadd64(&gabgcstats.refsSkipped, 1)
		return false
	}
	src := *(*uintptr)(unsafe.Pointer(addr))
	if src == 0 {
		atomic.Xadd64(&gabgcstats.refsSkipped, 1)
		return false
	}
	s := spanOf(src)
	if s == nil || !s.isGab() {
		atomic.Xadd64(&gabgcstats.refsSkipped, 1)
		return false
	}
	// Skip pinned GABs. This covers two cases.
	// First, skip active GABs, which are pinned before evacuation.
	// Second, skip survivor GABs. Duplicated addrs could appear in
	// the refsIn queue. During processing the first addr, the GAB
	// object referred by src has been relocated to the survivor GAB,
	// and the next one should be skipped, since now *addr points to
	// the non-forwareded relocated object. Survivor GABs are pinned,
	// since they are new objects allocated during marking, thereby
	// relocated GAB objects are skipped here.
	if s.isPinnedGab(src) {
		atomic.Xadd64(&gabgcstats.refsSkipped, 1)
		return false
	}
	gabStart := s.base() + s.objIndex(src)*s.elemsize
	blockStart, size := gabFindBlockFast(src, gabStart)

	// Evacuated GABs do not contain any live objects,
	// so they can be swept in the current GC cycle.
	markBit := s.markBitsForIndex(s.objIndex(src))
	if markBit.isMarked() {
		markBit.clearMarked()
		atomic.Xadduintptr(&gabgcstats.gabEvacuatedBytes, s.elemsize)
	}

	// Do copying
	{
		var dest uintptr
		// src may point to the middle of object
		offset := src - blockStart
		// If the object is forwarded, it has been copied
		if isForwarded(s, blockStart) {
			dest = forwardee(s, blockStart) + offset
			atomic.Xadd64(&gabgcstats.refsUpdated, 1)
		} else {
			// The object is not copied yet
			newBlock := uintptr(sgab.stwMalloc(size))
			if newBlock == 0 {
				throw("Fail to allocate objects in suvrivor GABs")
			}
			dest = newBlock + offset
			// Do actual copying
			memmove(unsafe.Pointer(newBlock), unsafe.Pointer(blockStart), size)
			startObj(newBlock, sgab.span)
			// process any possible mspecials
			relocateSpecials(blockStart, newBlock, size)
			// Install forwarding pointer
			forwardTo(s, blockStart, newBlock)
			atomic.Xadd64(&gabgcstats.objCopied, 1)
			atomic.Xadd64(&gabgcstats.bytesCopied, int64(size))
		}
		// Update reference
		atomic.StoreReluintptr((*uintptr)(unsafe.Pointer(addr)), dest)
	}
	return true
}

// Use objStartBits to find the size of GAB-allocated objects.
// src points to the middle of a GAB object.
//
// Search to LHS (to lower address space) to find the beginning
// of the current object, say b, and then search to RHS (to higher
// address space) to find the beginning of the adjacent object,
// say up. Then (up - b) is the size of the current object.
//
// Finding the size of the last object in GABs should be specially
// handled, since there are no objects allocated on the RHS of it.
//
// For non-active GABs, i.e., retired GABs, by design, are
// full of objects. Consequently, during finding, up is both GAB's
// upper bound and the boundary of the last object, so (up - b)
// is the size of the last object that still holds.
//
// For active GABs, since they may not be full of objects, (up - b)
// is not the size of the last object anymore.
//
// Solutions
// 1. All the active GABs are pinned before GC marking. By design,
// pointers to objects in pinned GABs are skipped and not added to
// refsIn queues, so that their size is not examined. As new objects,
// GABs allocated during marking have already been pinned;
// 2. Once a g is dead, its GAB is retired. Mark the top of the
// GAB as the beginning of an object;
// 3. After evacuation, mark the top of the last survivor GAB
// as a beginning of an object.
//
// Note that marking the top of active GABs as object beginning
// makes GABs parsable. It is safe and not ambiguous, since it
// is what it means once actual allocation happens. Furthermore,
// no one refers to the "dummy object" at the end of each GAB.
//
//go:nowritebarrierrec
func gabFindBlockFast(src, lbound uintptr) (b, size uintptr) {
	s := spanOf(src)
	// Clear lower three bits
	p := src &^ 7

	// Search to lower address space to find the beginning of current object.
	bytep, curMask := s.objStartBitsForIndex((p - s.base()) / goarch.PtrSize)
	lowByte, _ := s.objStartBitsForIndex((lbound - s.base()) / goarch.PtrSize)
	var offset int
	// Clear possible objStartBit before current bit.
	// Note that current bit is not masked.
	before := sys.LeadingZeros8(curMask)
	mask := byte(0xff) >> before
	bt := *bytep & mask
	// Count the offset from the current bit to the first objStartBit.
	offset = sys.LeadingZeros8(bt) - before
	// objStartBit is not found in current byte. Move to the next byte.
	if bt == 0 {
		bytep = uint8PtrAdd(bytep, -1)
		for ; uintptr(unsafe.Pointer(bytep)) >= uintptr(unsafe.Pointer(lowByte)); bytep = uint8PtrAdd(bytep, -1) {
			bt = *bytep
			if bt != 0 {
				offset += sys.LeadingZeros8(bt)
				break
			} else {
				offset += 8
			}
		}
	}
	b = p - uintptr(offset)*goarch.PtrSize

	offset = 0
	ubound := lbound + s.elemsize
	bytep, curMask = s.objStartBitsForIndex((p - s.base()) / goarch.PtrSize)
	highBit, _ := s.objStartBitsForIndex((ubound - s.base()) / goarch.PtrSize)
	// Clear possible objStartBit after current bit.
	after := sys.TrailingZeros8(curMask)
	mask = byte(0xff) << (after + 1)
	bt = *bytep & mask
	offset = sys.TrailingZeros8(bt) - after
	if bt == 0 {
		bytep = uint8PtrAdd(bytep, 1)
		for ; uintptr(unsafe.Pointer(bytep)) < uintptr(unsafe.Pointer(highBit)); bytep = uint8PtrAdd(bytep, 1) {
			bt = *bytep
			if bt != 0 {
				offset += sys.TrailingZeros8(bt)
				break
			} else {
				offset += 8
			}
		}
	}
	up := p + uintptr(offset)*goarch.PtrSize
	size = up - b
	if size == 0 {
		throw("Object size cannot be zero.")
	}
	return
}

func uint8PtrAdd(bytep *uint8, delta int) *uint8 {
	return (*uint8)(unsafe.Add(unsafe.Pointer(bytep), delta))
}

// Pin all the active GABs before marking.
//
//go:nowritebarrierrec
func gabGCPrologue() {
	lock(&allglock)
	for _, gp := range allgs {
		if ab := uintptr(gp.ab.base); ab != 0 {
			spanOf(ab).gabPinObj(ab)
		}
	}
	unlock(&allglock)

	for mp := allm; mp != nil; mp = mp.alllink {
		if ab := uintptr(mp.g0.ab.base); ab != 0 {
			spanOf(ab).gabPinObj(ab)
		}
	}
}

// Do cleaning job after evacuation, including:
// 1. cleaning metadata of survivor GABs;
// 2. freeing reference queue.
// Note that objStartBits of active GABs have been
// relocated before evacuation. Other gcBits are
// relocated during sweeping.
func gabGCEpilogue() {
	assertWorldStopped()
	gabClearStwChunks()
	// Destroy all the reference queues
	var nq uintptr
	for _, pp := range allp {
		nq += pp.refsIn.destroy()
	}
	if gabdebug.trace >= 1 && nq > 0 {
		println("[GAB: total", nq, "reference blocks destroyed]")
	}

	memstats.numbalancedgc++
	// Print GAB relocation log
	if gabdebug.trace >= 1 {
		gabgcstats.print()
	}
	gabgcstats.check()
	gabgcstats.reset()
}

// Common mallocgc WITHOUT GAB allocation
// GAB copying moves noscan objects.
// However, some code invokes mallocgc() directly and
// pass a nil type to allocate a piece of memory, then
// casts it to the desired type. This object is a noscan
// one and will be allocated in a GAB. If this object
// contains a pointer to a GAB object, GAB copying is
// unable to update pointer containing in this object,
// since this object will not be scanned during marking.
// Therefore, for such case, do not allocate it in GABs.
func mallocgc2(size uintptr, typ *_type, needzero bool) unsafe.Pointer {
	if doubleCheckMalloc {
		if gcphase == _GCmarktermination {
			throw("mallocgc called with gcphase == _GCmarktermination")
		}
	}

	if isEvacEnabled() && gabIsEvacuating {
		throw("Allocation happens during GAB evacuation")
	}

	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}

	return mallocgc1WithoutSizeSpecializedMalloc(size, typ, needzero, false)
}

// mallocgc1WithoutSizeSpecializedMalloc remove the SizeSpecializedMalloc path in mallocgc1
// The GAB allocation path is also added to SizeSpecializedMalloc generated code, so we
// need to remove the SizeSpecializedMalloc path to disable gab for mallocgc2.
func mallocgc1WithoutSizeSpecializedMalloc(size uintptr, typ *_type, needzero bool, isgab bool) unsafe.Pointer {
	// It's possible for any malloc to trigger sweeping, which may in
	// turn queue finalizers. Record this dynamic lock edge.
	// N.B. Compiled away if lockrank experiment is not enabled.
	lockRankMayQueueFinalizer()
	gp := getg()

	// Pre-malloc debug hooks.
	if debug.malloc {
		if x := preMallocgcDebug(size, typ); x != nil {
			return x
		}
	}

	// For ASAN, we allocate extra memory around each allocation called the "redzone."
	// These "redzones" are marked as unaddressable.
	var asanRZ uintptr
	if asanenabled {
		asanRZ = redZoneSize(size)
		size += asanRZ
	}

	// Assist the GC if needed. (On the reuse path, we currently compensate for this;
	// changes here might require changes there.)
	if gcBlackenEnabled != 0 {
		deductAssistCredit(size)
	}

	// Actually do the allocation.
	var x unsafe.Pointer
	var elemsize uintptr
	if size <= maxSmallSize-gc.MallocHeaderSize {
		if typ == nil || !typ.Pointers() {
			// tiny allocations might be kept alive by other co-located values.
			// Make sure secret allocations get zeroed by avoiding the tiny allocator
			// See go.dev/issue/76356
			gp := getg()
			if size < maxTinySize && gp.secret == 0 {
				x, elemsize = mallocgcTiny(size, typ)
			} else {
				x, elemsize = mallocgcSmallNoscan(size, typ, needzero, isgab)
			}
		} else {
			if !needzero {
				throw("objects with pointers must be zeroed")
			}
			if heapBitsInSpan(size) {
				x, elemsize = mallocgcSmallScanNoHeader(size, typ)
			} else {
				x, elemsize = mallocgcSmallScanHeader(size, typ)
			}
		}
	} else {
		x, elemsize = mallocgcLarge(size, typ, needzero)
	}

	if goexperiment.RuntimeSecret && gp.secret > 0 {
		// Mark any object allocated while in secret mode as secret.
		// This ensures we zero it immediately when freeing it.
		addSecret(x, elemsize)
	}

	// Notify sanitizers, if enabled.
	if raceenabled {
		racemalloc(x, size-asanRZ)
	}
	if msanenabled {
		msanmalloc(x, size-asanRZ)
	}
	if asanenabled {
		// Poison the space between the end of the requested size of x
		// and the end of the slot. Unpoison the requested allocation.
		frag := elemsize - size
		if typ != nil && typ.Pointers() && !heapBitsInSpan(elemsize) && size <= maxSmallSize-gc.MallocHeaderSize {
			frag -= gc.MallocHeaderSize
		}
		asanpoison(unsafe.Add(x, size-asanRZ), asanRZ)
		asanunpoison(x, size-asanRZ)
	}
	if valgrindenabled {
		valgrindMalloc(x, size-asanRZ)
	}

	// Adjust our GC assist debt to account for internal fragmentation.
	if gcBlackenEnabled != 0 && elemsize != 0 {
		if assistG := getg().m.curg; assistG != nil {
			assistG.gcAssistBytes -= int64(elemsize - size)
		}
	}

	// Post-malloc debug hooks.
	if debug.malloc {
		postMallocgcDebug(x, elemsize, typ)
	}
	return x
}

// Do not allocate the bucket array in the GAB.
func newBucketArray(typ *_type, n int) unsafe.Pointer {
	if n == 1 {
		return mallocgc2(typ.Size_, typ, true)
	}
	mem, overflow := math.MulUintptr(typ.Size_, uintptr(n))
	if overflow || mem > maxAlloc || n < 0 {
		panic(plainError("runtime: allocation size out of range"))
	}
	return mallocgc2(mem, typ, true)
}

//go:linkname mapsNewGroupArray internal/runtime/maps.newGroupArray
func mapsNewGroupArray(typ *_type, n int) unsafe.Pointer {
	return newBucketArray(typ, n)
}

// Find if the object being copied contains any mspecials.
// If true, relocate special blocks to the dest span and fix their offsets.
//
//go:nowritebarrierrec
func relocateSpecials(src uintptr, dest uintptr, size uintptr) {
	srcSpan := spanOf(src)
	if srcSpan.specials == nil {
		return
	}
	destSpan := spanOf(dest)
	srcOffset := src - srcSpan.base()
	objStartOffset := srcOffset
	objEndOffset := srcOffset + size

	// Check if whether the copied object has any special blocks
	lock(&srcSpan.speciallock)
	var oriStartPrev *special
	var oriStart *special
	var oriEnd *special
	var count int
	{
		var prev, cur *special
		for prev, cur = nil, srcSpan.specials; cur != nil && cur.offset < objStartOffset; prev, cur = cur, cur.next {
		}
		if cur != nil && cur.offset < objEndOffset {
			oriStartPrev = prev
			oriStart = cur
			for ; cur != nil && cur.offset < objEndOffset; cur = cur.next {
				count++
				oriEnd = cur
			}
		}
	}
	if oriStart == nil && oriEnd == nil {
		unlock(&srcSpan.speciallock)
		return
	}
	oriStartOffset := oriStart.offset
	oriEndOffset := oriEnd.offset

	// Find positions for special blocks in the dest mspan
	destOffset := func(offset uintptr, s *mspan, d *mspan, ori uintptr, dest uintptr) uintptr {
		blockOffset := offset - (ori - s.base())
		return dest - d.base() + blockOffset
	}

	if destSpan != srcSpan {
		lock(&destSpan.speciallock)
	}
	var destStart *special
	var destEnd *special
	destStartOffset := destOffset(oriStart.offset, srcSpan, destSpan, src, dest)
	if destSpan.specials == nil || destSpan.specials.offset > destStartOffset {
		destStart = nil
	} else {
		var prev, cur *special
		for prev, cur = nil, destSpan.specials; cur != nil && cur.offset < destStartOffset; prev, cur = cur, cur.next {
		}
		destStart = prev
		destEnd = cur
	}

	// Remove special blocks from the target mspan
	if oriStartPrev == nil {
		if oriStart != srcSpan.specials {
			unlock(&srcSpan.speciallock)
			if destSpan != srcSpan {
				unlock(&destSpan.speciallock)
			}
			throw("Original special block should be the first one of the special list")
		}
		srcSpan.specials = oriEnd.next
	} else {
		oriStartPrev.next = oriEnd.next
	}
	// Mark the src span does not have specials now
	if srcSpan.specials == nil {
		spanHasNoSpecials(srcSpan)
	}
	// Mark the dest span has specials now
	if destSpan.specials == nil {
		spanHasSpecials(destSpan)
	}

	// Insert special blocks into the target mspan
	if destStart == nil {
		old := destSpan.specials
		destSpan.specials = oriStart
		oriEnd.next = old
	} else {
		oriEnd.next = destStart.next
		destStart.next = oriStart
	}

	// Fix offsets
	for cur := oriStart; ; cur = cur.next {
		cur.offset = destOffset(cur.offset, srcSpan, destSpan, src, dest)
		if cur == oriEnd {
			break
		}
	}

	// Check offsets
	if destEnd != nil && oriEnd.offset >= destEnd.offset {
		unlock(&srcSpan.speciallock)
		if destSpan != srcSpan {
			unlock(&destSpan.speciallock)
		}
		throw("Incorrect dest special block offset")
	}

	unlock(&srcSpan.speciallock)
	if destSpan != srcSpan {
		unlock(&destSpan.speciallock)
	}

	if gabdebug.trace >= 3 {
		destObjStartOffset := uint16(dest - destSpan.base())
		destObjEndOffset := uint16(dest + size - destSpan.base())
		print("[GAB special relocation: src span=", srcSpan, ", src object offset start=", objStartOffset, ", end=", objEndOffset, ", number of special copied=", count,
			", src special offset start=", oriStartOffset, ", src special offset end=", oriEndOffset,
			", dest span=", destSpan, ", dest object offset start=", destObjStartOffset, ", end=", destObjEndOffset,
			", dest special offset start=", oriStart.offset, ", dest special offset end=", oriEnd.offset, "]\n")
	}
}

// Check if the key of a map is a pointer or is an object with a pointer.
// Go uses AES encoding of the raw memory of an object as its hash code.
// Since GAB evacuation relocates objects, the address of an object is
// altered. Therefore, for objects containing pointers to GAB objects,
// their content changed and their hash code also changed. Consequently,
// despite an object being in the map, one may fail to get it from the
// hash map according to the changed hash code.
// To solve this, map buckets whose keys contain pointers are marked as
// frozen. During GC marking, GAB objects pointed by frozen buckets
// are over-approximately marked as pinned, so that they will not be
// copied by balanced GC.
// Meanwhile, when a map is cloned by calling maps.Clone(), cloned map
// buckets are also marked as frozen if the original ones are frozen.
func pinMapBucket(t *abi.MapType, bucket unsafe.Pointer) {
	if t.Key.PtrBytes != 0 {
		frozenBitsForAddr(uintptr(bucket)).setMarked()
	}
}

//go:linkname mapsPinSwissMapGroups internal/runtime/maps.pinSwissMapGroups
func mapsPinSwissMapGroups(t *abi.MapType, bucket unsafe.Pointer) {
	pinMapBucket(t, bucket)
}

func frozenBitsForAddr(p uintptr) markBits {
	s := spanOf(p)
	objIndex := s.objIndex(p)
	return s.frozenBitsForIndex(objIndex)
}

func (s *mspan) frozenBitsForIndex(objIndex uintptr) markBits {
	if s.loadFrozenBits() == nil {
		lock(&s.frozenBitsLock)
		if s.loadFrozenBits() == nil {
			s.newFrozenBits()
		}
		unlock(&s.frozenBitsLock)
	}
	bytep, mask := s.loadFrozenBits().bitp(objIndex)
	return markBits{bytep, mask, objIndex}
}

func (s *mspan) shouldBePinned(addr uintptr) bool {
	if s.loadFrozenBits() != nil {
		return frozenBitsForAddr(addr).isMarked()
	}
	return false
}

// Use fixalloc to allocate frozenBits.
// frozenBits mark map buckets containing pointers.
// A map bucket leads with eight uint8 tophash,
// then eight key-value pairs, and ends with an
// overflow pointer. For a map bucket that can
// have frozenBits, the minimal size is 80 bytes:
// tophash: 1B*8
// A pointer-type key (without a value): 8B*8
// an overflow pointer: 8B
//
// According to the spec of mspan, for an 80 bytes object,
// the corresponding mspan has 102 objects,
// i.e., 102 bits frozenBits. Considering gcBits are
// 8B-aligned, the maximal size of frozenBits is 16 bytes.
// For larger objects, the size of frozenBits is 8 bytes.
// For convenience, we use a 16B fixalloc to allocate frozenBits.
func (s *mspan) newFrozenBits() {
	lock(&mheap_.frozenAllocLock)
	// Calculate the size of frozenBits in bytes
	// This logic should be aligned with newMarkBits()
	// Assure the atomicity and sequential consistency of updating frozenBytes and frozenBits
	// A race condition may occur when newFrozenBits() and updateFrozenBits()
	// run concurrently. Update frozenBytes first then frozenBits. If frozenBits
	// is not nil, then the up-to-date value of frozenBytes must be visible.
	blocksNeeded := (uintptr(s.nelems) + 63) / 64
	s.frozenBytes = blocksNeeded * 8
	s.storeFrozenBits((*gcBits)(mheap_.frozenBitAlloc.alloc()))
	unlock(&mheap_.frozenAllocLock)

}

// frozenBits are freed when the corresponding mspan is freed.
func (s *mspan) freeFrozenBits() {
	lock(&mheap_.frozenAllocLock)
	mheap_.frozenBitAlloc.free(unsafe.Pointer(s.frozenBits))
	unlock(&mheap_.frozenAllocLock)
	s.storeFrozenBits(nil)
}

// Bitwise AND with allocBits after sweeping so that the frozenBits
// of unused objects are cleared. Since the size of frozenBytes is
// either 8 bytes or 16 bytes, convert them to uint64 and perform
// one or two bitwise AND operations.
func (s *mspan) updateFrozenBits() {
	frozenBit := (*uint64)(unsafe.Pointer(s.frozenBitsForIndex(0).bytep))
	allocBit := (*uint64)(unsafe.Pointer(s.allocBitsForIndex(0).bytep))
	if s.frozenBytes == 8 {
		*frozenBit &= *allocBit
	} else if s.frozenBytes == 16 {
		*frozenBit &= *allocBit
		frozenBit = next(frozenBit)
		allocBit = next(allocBit)
		*frozenBit &= *allocBit
	} else {
		println("Unexpected length of frozenBits", s.frozenBytes)
		throw("")
	}
}

//go:nosplit
func next(ptr *uint64) *uint64 {
	return (*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + 8))
}

// Use fixalloc to allocate objStartBits.
// objStartBits are attached to each GAB mspan and
// mark the beginning of objects allocated in GABs.
// objStartBits should be preserved across GC as long as
// there are live objects in the GAB, so that we use
// fixalloc to allocate off-heap memory for them to
// avoid copying them across GC.
//
// The size of a GAB can be 1KB, 2KB, 4KB, 8KB, 16KB and 32KB,
// and the size of corresponding mspan is 8KB, 16KB and 32KB.
// Each 8-byte memory block in mspan corresponds to 1 bit in
// the objStartBits. Therefore, the total size of objStartBits
// for three kinds of mspans are 128B, 256B and 512B.
// For convenience, we use two sizes of fixalloc, 256B and 512B,
// to allocate objStartBits.
func (s *mspan) newObjStartBits() {
	if s.elemsize <= gabCapMax {
		lock(&mheap_.smallObjStartLock)
		s.objStartBits = (*gcBits)(mheap_.smallObjStartAlloc.alloc())
		unlock(&mheap_.smallObjStartLock)
	} else if s.elemsize == maxSmallSize {
		lock(&mheap_.largeObjStartLock)
		s.objStartBits = (*gcBits)(mheap_.largeObjStartAlloc.alloc())
		unlock(&mheap_.largeObjStartLock)
	} else {
		println("Unexpected GAB size", s.elemsize)
		throw("")
	}
}

//go:nowritebarrierrec
func (s *mspan) newGabEvacBits() {
	nelems := uintptr(s.nelems) * s.elemsize / goarch.PtrSize
	s.gabEvacBits = newMarkBits(nelems)
}

//go:nowritebarrierrec
func (s *mspan) gabEvacBitsForAddr(addr uintptr) markBits {
	if s.gabEvacBits == nil {
		lock(&s.gabEvacBitsLock)
		if s.gabEvacBits == nil {
			s.newGabEvacBits()
		}
		unlock(&s.gabEvacBitsLock)
	}
	index := (addr - s.startAddr) / goarch.PtrSize
	bytep, mask := s.gabEvacBits.bitp(index)
	return markBits{bytep, mask, index}
}

//go:nowritebarrierrec
func isMarked(s *mspan, objStart uintptr) bool {
	return s.gabEvacBitsForAddr(objStart).isMarked()
}

//go:nowritebarrierrec
func setMarked(s *mspan, objStart uintptr) {
	s.gabEvacBitsForAddr(objStart).setMarked()
}

// TODO Thread-safe of concurrent evacuation
//
//go:nowritebarrierrec
func (s *mspan) switchToFwdBits() {
	nelems := uintptr(s.nelems) * s.elemsize / goarch.PtrSize
	blocksNeeded := (nelems + 63) / 64
	bytesNeeded := blocksNeeded * 8
	ptr := unsafe.Pointer(s.gabEvacBits)
	memclrNoHeapPointers(ptr, bytesNeeded)
	s.isFwdBits = true
}

//go:nowritebarrierrec
func isForwarded(s *mspan, objStart uintptr) bool {
	if !s.isFwdBits {
		s.switchToFwdBits()
	}
	return s.gabEvacBitsForAddr(objStart).isMarked()
}

//go:nowritebarrierrec
func forwardTo(s *mspan, objStart, forwardee uintptr) {
	s.gabEvacBitsForAddr(objStart).setMarked()
	dest := (*uintptr)((unsafe.Pointer)(objStart))
	*dest = forwardee
}

//go:nowritebarrierrec
func forwardee(s *mspan, objStart uintptr) uintptr {
	if !isForwarded(s, objStart) {
		dumpObject("obj", objStart)
		throw("object should have been forwarded")
	}
	return *(*uintptr)((unsafe.Pointer)(objStart))
}

//go:nowritebarrierrec
func dumpObject(label string, objStart uintptr) {
	s := spanOf(objStart)
	if s != nil {
		index := s.objIndex(objStart)
		start := s.base() + s.elemsize*index
		gcDumpObject(label, start, objStart-start)
	}
}

// objStartBis are freed when the corresponding mspan is freed.
func (s *mspan) freeObjStartBits() {
	if s.elemsize <= gabCapMax {
		lock(&mheap_.smallObjStartLock)
		mheap_.smallObjStartAlloc.free(unsafe.Pointer(s.objStartBits))
		unlock(&mheap_.smallObjStartLock)
	} else if s.elemsize == maxSmallSize {
		lock(&mheap_.largeObjStartLock)
		mheap_.largeObjStartAlloc.free(unsafe.Pointer(s.objStartBits))
		unlock(&mheap_.largeObjStartLock)
	} else {
		println("Unexpected GAB size", s.elemsize)
		throw("")
	}
	s.objStartBits = nil
}

func (s *mspan) objStartBitsForIndex(index uintptr) (bytep *uint8, mask uint8) {
	return s.objStartBits.bitp(index)
}

func (s *mspan) isObjStart(index uintptr) bool {
	bytep, mask := s.objStartBitsForIndex(index)
	return (*bytep)&(mask) != 0
}

// It is safe to use a plain store, rather than an atomic store
// like markBits.setMarked(), to set the objStartBits.
// 1. The objStartBits is allocated for each mspan. Each GAB of
// a mspan corresponds to a byte-aligned objStartBits memory
// block, which is not shared across GABs.
// 2. For active GABs, there is only one goroutine writing the
// objStartBits, and at the same time, there is no goroutine
// reading it. This is guaranteed by the GAB pinning mechanism.
// 3. For retired GABs, accessing objStartBits of them is always
// happens after the GC stop-the-world, whose underlying
// synchronization semantics assures that the objStartBits are
// up-to-date.
//
//go:nosplit
func startObj(start uintptr, s *mspan) {
	index := (start - s.base()) / goarch.PtrSize
	bytep, mask := s.objStartBitsForIndex(index)
	*bytep |= mask
}

// Clear objStartBits of freed GABs after sweeping
func (s *mspan) clrFreeObjStartBits() {
	elemSize := s.elemsize
	bytesNeeded := elemSize / goarch.PtrSize / 8
	for i := uintptr(0); i < uintptr(s.nelems); i++ {
		if s.isFree(i) {
			startIndex := i * elemSize / goarch.PtrSize
			src, _ := s.objStartBits.bitp(startIndex)
			memclrNoHeapPointers(unsafe.Pointer(src), bytesNeeded)
		}
	}
}

// Rescan all the stacks before GAB evacuation is performed.
//
//go:nowritebarrierrec
func checkAllG() {
	gabgcstats.rescanStartNs = nanotime()
	var gs uint64
	lock(&allglock)
	for _, gp := range allgs {
		s := readgstatus(gp)
		// g should be in one of the following status:
		// _Gdead, the goroutine is unused;
		// _Grunnable, the goroutine is in the runnable queue;
		// _Gwaiting, the goroutine is blocked by stopTheWorldWithSema();
		// _Gsyscall, the goroutine is executing a system call.
		if s == _Gdead {
			continue
		}
		gs++
		rescanStack(gp)
	}
	unlock(&allglock)
	// Note that g0s are not in the allgs.
	// Iterate over m and check corresponding g0.
	// In GAB evacuation GC cycles, pin active GABs before evacuation.
	// Active GABs are not evacuated, since objects in it are recently
	// allocated and are likely to be alive.
	for mp := allm; mp != nil; mp = mp.alllink {
		if gb := uintptr(mp.g0.ab.base); gb != 0 {
			spanOf(gb).gabPinObj(gb)
			gs++
		}
	}
	atomic.Store64(&gabgcstats.rescannedG, gs)
	gabgcstats.rescanEndNs = nanotime()
}

// If marking is performed concurrently, some GAB pointers could appear in
// the stack after the stack has been scanned. Consequently, GAB objects
// pointed by those pointers are not marked as pinned, thereby copied by
// balanced GC during evacuation. GAB pointers in stacks are unable to be
// updated by balanced GC, so that two copies of one object appears,
// which is a severe issue.
//
// During mark termination, all the stacks are rescanned before GAB
// evacuation, so newly appeared GAB pointers in stacks can be found
// and those GAB objects are marked as not copied.
//
//go:nowritebarrierrec
func rescanStack(gp *g) {
	// Active GABs are not evacuated, since objects in it
	// are recently allocated and are likely to be alive.
	if gb := uintptr(gp.ab.base); gb != 0 {
		spanOf(gb).gabPinObj(gb)
	}

	var state stackScanState
	state.stack = gp.stack

	var u unwinder
	for u.init(gp, 0); u.valid(); u.next() {
		scanStackFrame(&u.frame, &state)
	}
}

func scanStackFrame(frame *stkframe, state *stackScanState) {
	isAsyncPreempt := frame.fn.valid() && frame.fn.funcID == abi.FuncID_asyncPreempt
	isDebugCall := frame.fn.valid() && frame.fn.funcID == abi.FuncID_debugCallV2
	if state.conservative || isAsyncPreempt || isDebugCall {
		if frame.varp != 0 {
			size := frame.varp - frame.sp
			if size > 0 {
				scanFrameBlock(frame.sp, size, nil)
			}
		}

		if n := frame.argBytes(); n != 0 {
			scanFrameBlock(frame.argp, n, nil)
		}

		if isAsyncPreempt || isDebugCall {
			state.conservative = true
		} else {
			state.conservative = false
		}
		return
	}

	locals, args, objs := frame.getStackMap(false)

	if locals.n > 0 {
		size := uintptr(locals.n) * goarch.PtrSize
		scanFrameBlock(frame.varp-size, size, locals.bytedata)
	}

	if args.n > 0 {
		scanFrameBlock(frame.argp, uintptr(args.n)*goarch.PtrSize, args.bytedata)
	}

	// Check all stack objects whether they point to GAB objects.
	if frame.varp != 0 {
		// varp is 0 for defers, where there are no locals.
		// In that case, there can't be a pointer to its args, either.
		// (And all args would be scanned above anyway.)
		for i := range objs {
			obj := &objs[i]
			off := obj.off
			base := frame.varp // locals base pointer
			if off >= 0 {
				base = frame.argp // arguments and return values base pointer
			}
			ptr := base + uintptr(off)
			if ptr < frame.sp {
				// object hasn't been allocated in the frame yet.
				continue
			}
			ptrBytes, gcdata := obj.gcdata()
			scanFrameBlock(ptr, ptrBytes, gcdata)
		}
	}
}

//go:nowritebarrierrec
func scanFrameBlock(b, n uintptr, ptrmask *uint8) {
	for i := uintptr(0); i < n; i += goarch.PtrSize {
		if ptrmask != nil {
			word := i / goarch.PtrSize
			bits := *addb(ptrmask, word/8)
			if bits == 0 {
				if i%(goarch.PtrSize*8) != 0 {
					throw("misaligned mask")
				}
				i += goarch.PtrSize*8 - goarch.PtrSize
				continue
			}
			if (bits>>(word%8))&1 == 0 {
				continue
			}
		}
		ptr := *(*uintptr)(unsafe.Pointer(b + i))
		pinIfOnHeap(ptr)
	}
}

// Function getSwissMapGroupPtrs pushed from internal/runtime/maps.
//
//go:linkname getSwissMapGroupPtrs
func getSwissMapGroupPtrs(m unsafe.Pointer) []unsafe.Pointer
