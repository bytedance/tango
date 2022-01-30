// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/runtime/atomic"
	"internal/runtime/sys"
	"unsafe"
)

const (
	slotsPerBlock = 128
)

type ref uintptr

func (r ref) deref() ref {
	return *(*ref)(unsafe.Pointer(r))
}

type refq struct {
	count uint64
	head  uintptr
}

type refBlock struct {
	_         sys.NotInHeap
	nextBlock uintptr
	nextIndex uint64
	ptrs      [slotsPerBlock]uintptr
}

//go:nowritebarrierrec
//go:systemstack
func allocRefBlock() (b *refBlock) {
	lock(&mheap_.lock)
	b = (*refBlock)(mheap_.refsalloc.alloc())
	unlock(&mheap_.lock)
	if b.nextIndex != 0 || b.nextBlock != 0 {
		throw("refBlock should have been zeroed")
	}
	return
}

//go:nowritebarrierrec
//go:systemstack
func freeRefBlock(b *refBlock) {
	lock(&mheap_.lock)
	mheap_.refsalloc.free(unsafe.Pointer(b))
	unlock(&mheap_.lock)
}

// Enqueue the address of a reference to a GAB-allocated object,
// which will be processed during GAB evacuation.
//
//go:nowritebarrierrec
//go:systemstack
func (q *refq) enqueue(addr uintptr) {
	if gcphase != _GCmark {
		throw("Enqueue should happen only during marking")
	}

	r := ref(addr).deref()
	if r == 0 {
		return
	}
	s := spanOf(uintptr(r))
	if s == nil || !s.isGab() {
		return
	}
	if q.head == 0 {
		q.head = uintptr(unsafe.Pointer(allocRefBlock()))
	}

	b := (*refBlock)(unsafe.Pointer(q.head))
	if b == nil {
		throw("Block does not exist")
	}
	b.ptrs[b.nextIndex] = addr
	b.nextIndex++
	q.count++
	atomic.Xadd64(&gabgcstats.refsEnqueued, 1)

	// If current block is full, allocate a new one.
	if b.nextIndex >= slotsPerBlock {
		newHead := allocRefBlock()
		newHead.nextBlock = q.head
		q.head = uintptr(unsafe.Pointer(newHead))
	}
}

//go:nowritebarrierrec
//go:systemstack
func (q *refq) ensureCapacity() {
	if q.head == 0 {
		q.head = uintptr(unsafe.Pointer(allocRefBlock()))
	} else {
		b := (*refBlock)(unsafe.Pointer(q.head))
		if b.nextIndex >= slotsPerBlock {
			newHead := allocRefBlock()
			newHead.nextBlock = q.head
			q.head = uintptr(unsafe.Pointer(newHead))
		}
	}
}

//go:nowritebarrierrec
//go:systemstack
func (q *refq) destroy() (nb uintptr) {
	assertWorldStopped()
	if gcphase != _GCoff {
		throw("destroy should happen only in phase _GCoff")
	}
	if gabdebug.trace != 0 {
		q.touchBlocks()
	}

	for b := q.head; b != 0; {
		blk := (*refBlock)(unsafe.Pointer(b))
		b = blk.nextBlock
		nb++
		freeRefBlock(blk)
	}
	q.count = 0
	q.head = 0
	return
}

//go:nowritebarrierrec
//go:systemstack
func (q *refq) refsDo(doRef func(addr uintptr) bool) {
	var refsProcessed int64
	for b := q.head; b != 0; {
		blk := (*refBlock)(unsafe.Pointer(b))
		for i := uint64(0); i < blk.nextIndex; i++ {
			addr := blk.ptrs[i]
			doRef(addr)
			refsProcessed++
		}
		b = blk.nextBlock
	}
	atomic.Xadd64(&gabgcstats.refsProcessed, refsProcessed)
}

//go:nowritebarrierrec
//go:systemstack
func (q *refq) touchBlocks() (addr uintptr) {
	for b := q.head; b != 0; {
		blk := (*refBlock)(unsafe.Pointer(b))
		for i := uint64(0); i < blk.nextIndex; i++ {
			addr = blk.ptrs[i]
		}
		b = blk.nextBlock
	}
	return addr
}
