// Copyright 2014 The Go Authors. All rights reserved.
// Copyright (c) 2022 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2022-12-16.

package runtime

import "internal/abi"

func memdebuginit() {
	if debug.largealloctrace > 0 {
		if debug.largealloctrace <= maxSmallSize {
			debug.largealloctrace = maxSmallSize + 1
		}
	}
}

// TODO: Record the time consumed by large memory malloc(mmap + memclr).
func recordAllocLarge(size uintptr) {
	_p_ := getg().m.p.ptr()
	if _p_ == nil {
		return
	}
	nlargeStk := callers(5, _p_.largeAllocStk[:])
	var buf [24]byte
	sizeStr := string(itoa(buf[:], uint64(size)))
	sec, nsec, _ := time_now()
	unixNowMs := sec*1e3 + int64(nsec/1e6)
	unixStr := string(itoa(buf[:], uint64(unixNowMs)))
	printlock()
	print("[Memory] largeAllocObject bytes=", sizeStr,
		" timestamp=", unixStr, " stack=")
	_p_.largeAllocStkFrame.callers = _p_.largeAllocStk[:nlargeStk]
	_p_.largeAllocStkFrame.frames = _p_.largeAllocStkFrame.frameStore[:0]
	for {
		frame, more := _p_.largeAllocStkFrame.nextWithoutMalloc()
		if !more {
			print(frame.Function, "[", frame.File, ":", string(itoa(buf[:], uint64(frame.Line))), "]\n")
			break
		}
		print(frame.Function, "[", frame.File, ":", string(itoa(buf[:], uint64(frame.Line))), "]<-")
	}
	printunlock()
}

// Keep sync with src/runtime/symtab.go
// nextWithoutMalloc is similar to Next, but removes code that may cause memory allocation to avoid malloc deadlock.
// - skip expand cgo frames.
// - skip call funcNameForPrint on generic function.
func (ci *Frames) nextWithoutMalloc() (frame Frame, more bool) {
	for len(ci.frames) < 2 {
		// Find the next frame.
		// We need to look for 2 frames so we know what
		// to return for the "more" result.
		if len(ci.callers) == 0 {
			break
		}
		var pc uintptr
		if ci.nextPC != 0 {
			pc, ci.nextPC = ci.nextPC, 0
		} else {
			pc, ci.callers = ci.callers[0], ci.callers[1:]
		}
		funcInfo := findfunc(pc)
		if !funcInfo.valid() {
			// skip cgo frames to avoid malloc deadlock caused by grow slice.
			continue
		}
		f := funcInfo._Func()
		entry := f.Entry()
		if pc > entry {
			// We store the pc of the start of the instruction following
			// the instruction in question (the call or the inline mark).
			// This is done for historical reasons, and to make FuncForPC
			// work correctly for entries in the result of runtime.Callers.
			pc--
		}
		// It's important that interpret pc non-strictly as cgoTraceback may
		// have added bogus PCs with a valid funcInfo but invalid PCDATA.
		u, uf := newInlineUnwinder(funcInfo, pc)
		sf := u.srcFunc(uf)
		if u.isInlined(uf) {
			// Note: entry is not modified. It always refers to a real frame, not an inlined one.
			// File/line from funcline1 below are already correct.
			f = nil
			// When CallersFrame is invoked using the PC list returned by Callers,
			// the PC list includes virtual PCs corresponding to each outer frame
			// around an innermost real inlined PC.
			// We also want to support code passing in a PC list extracted from a
			// stack trace, and there only the real PCs are printed, not the virtual ones.
			// So check to see if the implied virtual PC for this PC (obtained from the
			// unwinder itself) is the next PC in ci.callers. If not, insert it.
			// The +1 here correspond to the pc-- above: the output of Callers
			// and therefore the input to CallersFrames is return PCs from the stack;
			// The pc-- backs up into the CALL instruction (not the first byte of the CALL
			// instruction, but good enough to find it nonetheless).
			// There are no cycles in implied virtual PCs (some number of frames were
			// inlined, but that number is finite), so this unpacking cannot cause an infinite loop.
			for unext := u.next(uf); unext.valid() && len(ci.callers) > 0 && ci.callers[0] != unext.pc+1; unext = u.next(unext) {
				snext := u.srcFunc(unext)
				if snext.funcID == abi.FuncIDWrapper && elideWrapperCalling(sf.funcID) {
					// Skip, because tracebackPCs (inside runtime.Callers) would too.
					continue
				}
				ci.nextPC = unext.pc + 1
				break
			}
		}
		ci.frames = append(ci.frames, Frame{
			PC:        pc,
			Func:      f,
			Function:  sf.name(), // remove call funcNameForPrint to avoid malloc deadlock caused by concatstrings.
			Entry:     entry,
			startLine: int(sf.startLine),
			funcInfo:  funcInfo,
			// Note: File,Line set below
		})
	}
	// Pop one frame from the frame list. Keep the rest.
	// Avoid allocation in the common case, which is 1 or 2 frames.
	switch len(ci.frames) {
	case 0: // In the rare case when there are no frames at all, we return Frame{}.
		return
	case 1:
		frame = ci.frames[0]
		ci.frames = ci.frameStore[:0]
	case 2:
		frame = ci.frames[0]
		ci.frameStore[0] = ci.frames[1]
		ci.frames = ci.frameStore[:1]
	default:
		frame = ci.frames[0]
		ci.frames = ci.frames[1:]
	}
	more = len(ci.frames) > 0
	if frame.funcInfo.valid() {
		// Compute file/line just before we need to return it,
		// as it can be expensive. This avoids computing file/line
		// for the Frame we find but don't return. See issue 32093.
		file, line := funcline1(frame.funcInfo, frame.PC, false)
		frame.File, frame.Line = file, int(line)
	}
	return
}
