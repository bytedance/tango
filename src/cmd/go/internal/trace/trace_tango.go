// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trace

import (
	"cmd/go/internal/base"
	"cmd/go/internal/cfg"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"internal/trace/traceviewer/format"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	ctPhaseAsyncBegin = "b"
	ctPhaseAsyncEnd   = "e"
)

type CTCategory uint8

const (
	CTHitPkgCache CTCategory = iota
	CTMissPkgCache
	CTHitVetCache
	CTMissVetCache
	CTHitOtherCache
	CTMissOtherCache
	CTHitCacheRate
	CTCompilerInfo
	CTCompilerError
)

var ctDebugTraceDir = os.Getenv("TANGO_DEBUG_TRACE")

func (cat CTCategory) String() string {
	switch cat {
	case CTHitPkgCache:
		return "hit-pkg-cache"
	case CTMissPkgCache:
		return "miss-pkg-cache"
	case CTHitVetCache:
		return "hit-vet-cache"
	case CTMissVetCache:
		return "miss-vet-cache"
	case CTHitOtherCache:
		return "hit-other-cache"
	case CTMissOtherCache:
		return "miss-other-cache"
	case CTHitCacheRate:
		return "hit-cache-rate"
	case CTCompilerInfo:
		return "compiler-info"
	case CTCompilerError:
		return "compiler-error"
	default:
		return "unknown"
	}
}

func (cat CTCategory) Desc() string {
	switch cat {
	case CTHitPkgCache:
		return "Hit Pkg Cache"
	case CTMissPkgCache:
		return "Miss Pkg Cache"
	case CTHitVetCache:
		return "Hit Vet Cache"
	case CTMissVetCache:
		return "Miss Vet Cache"
	case CTHitOtherCache:
		return "Hit Other Cache"
	case CTMissOtherCache:
		return "Miss Other Cache"
	case CTHitCacheRate:
		return "Cache Hit Rate"
	case CTCompilerInfo:
		return "Compiler Info"
	case CTCompilerError:
		return "Compiler Error"
	default:
		return "Unknown"
	}
}

type CTCacheHitArg struct {
	Pkg       string
	CacheFile []string
	CacheSize int64
}

type CTCacheMissArg struct {
	CTCacheHitArg
	CacheMissReason string
}

type CTCacheHitRateArg struct {
	CacheHitRate float64
}

type CTCompilerInfoArg struct {
	Args []string
	Envs []string
}

type CTCompilerErrorArg struct {
	Err                string
	PositionAndContent string
}

func CTEnableDebugTrace() bool {
	return ctDebugTraceDir != ""
}

func CTDisableDebugTrace() {
	ctDebugTraceDir = ""
}

func ctSortFile(f traceFile) error {
	if !CTEnableDebugTrace() {
		return nil
	}
	// We should conduct a sort of the files so that the alternating scenarios like
	// AStart-BStart-AEnd-BEnd are eliminated and only scenarios like AStart-AEnd-BStart-BEnd exist.
	if _, err := f.f.Seek(0, 0); err != nil {
		return err
	}
	b, err := io.ReadAll(f.f)
	if err != nil {
		return err
	}
	res := make([]format.Event, 0)
	if err := json.Unmarshal(b, &res); err != nil {
		return err
	}
	sort.SliceStable(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})
	l := len(res)
	for i := 0; i < l; i++ {
		if res[i].Phase != phaseDurationEnd {
			continue
		}
		for i+1 < l && res[i+1].Phase != phaseDurationBegin {
			res[i+1], res[i] = res[i], res[i+1]
			i++
		}
	}
	b, err = json.Marshal(res)
	if err != nil {
		return err
	}
	output, err := os.Create(strings.TrimSuffix(cfg.DebugTrace, "json") + "gz")
	if err != nil {
		return err
	}
	defer output.Close()
	w := gzip.NewWriter(output)
	defer w.Close()
	_, err = w.Write(b)
	return err
}

func (t *tracer) ctGetNextCacheID() uint64 {
	return atomic.AddUint64(&t.nextCacheID, 1)
}

// CTCache adds an asynchronous event to determine whether the package hits the cache.
func CTCache(ctx context.Context, name string, span *Span, cat CTCategory, arg any) {
	tc, ok := getTraceContext(ctx)
	if !ok {
		return
	}
	ev := &format.Event{
		Name:     name,
		Time:     float64(span.start.UnixNano()) / float64(time.Microsecond),
		TID:      tc.tid,
		Phase:    ctPhaseAsyncBegin,
		Category: cat.String(),
		ID:       tc.t.ctGetNextCacheID(),
		Arg:      arg,
	}
	tc.t.writeEvent(ev)
	ev.Phase = ctPhaseAsyncEnd
	ev.Time = float64(time.Now().UnixNano()) / float64(time.Microsecond)
	// Only one arg is needed for use to reduce the file size.
	ev.Arg = nil
	tc.t.writeEvent(ev)
}

func CTCreateAndSetDebugTrace(cmd *base.Command) {
	if !CTEnableDebugTrace() {
		return
	}
	if args := flag.Args(); len(args) != 0 {
		if args[0] != "build" && args[0] != "test" {
			CTDisableDebugTrace()
			return
		}
	}
	if err := os.MkdirAll(ctDebugTraceDir, 0755); err != nil {
		return
	}
	if cmd.CustomFlags {
		return
	}
	CTSetDebugTrace()
}

// CTSetDebugTrace ensures that if the -debug-trace option and the TANGO_DEBUG_TRACE environment variable are used
// simultaneously, additional extended information will be added to the -debug-trace file specified by the user.
func CTSetDebugTrace() {
	if !CTEnableDebugTrace() || cfg.DebugTrace != "" {
		return
	}
	ts := strconv.FormatInt(int64(time.Now().Nanosecond()), 10)
	if cfg.BuildO == "" {
		cfg.DebugTrace = path.Join(ctDebugTraceDir, "tango_debug_trace_"+ts) + ".json"
	} else {
		cfg.DebugTrace = path.Join(ctDebugTraceDir, path.Base(cfg.BuildO)+"_"+ts) + ".json"
	}
}
