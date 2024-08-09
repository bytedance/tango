// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package work

import (
	"cmd/go/internal/cache"
	"cmd/go/internal/trace"
	"context"
	"os"
)

func (b *Builder) ctRecordCacheByID(a *Action, key cache.ActionID, cat trace.CTCategory) {
	if !trace.CTEnableDebugTrace() {
		return
	}
	var (
		id string
		c  = cache.Default()
	)
	switch cac := c.(type) {
	case *cache.DiskCache:
		id = cac.CTGetDirHashFileName(key)
	case *cache.ProgCache:
	}
	file, _, err := cache.GetFile(c, key)
	if err != nil {
		return
	}
	fi, err := os.Stat(file)
	if err != nil {
		return
	}
	arg := &trace.CTCacheHitArg{
		Pkg:       a.Package.Desc(),
		CacheFile: []string{id, file},
		CacheSize: fi.Size(),
	}
	b.ctRecordCache(a, cat, arg)
}

func (b *Builder) ctRecordCache(a *Action, cat trace.CTCategory, arg any) {
	if a.Package == nil {
		return
	}
	if a.traceCtx == nil {
		return
	}
	desc := cat.Desc()
	switch cat {
	case trace.CTHitPkgCache:
		b.ctCacheHit.Add(1)
	case trace.CTMissPkgCache:
		b.ctCacheMiss.Add(1)
		arg = trace.CTCacheMissArg{
			CTCacheHitArg:   *arg.(*trace.CTCacheHitArg),
			CacheMissReason: a.Package.StaleReason,
		}
	}
	trace.CTCache(a.traceCtx, desc, a.traceSpan, cat, arg)
}

func (b *Builder) ctHitCacheRate(ctx context.Context, span *trace.Span) {
	if !trace.CTEnableDebugTrace() {
		return
	}
	hit := b.ctCacheHit.Load()
	miss := b.ctCacheMiss.Load()
	if hit+miss != 0 {
		trace.CTCache(ctx, trace.CTHitCacheRate.Desc(),
			span, trace.CTHitCacheRate, trace.CTCacheHitRateArg{CacheHitRate: float64(hit) / float64(hit+miss)})
	}
}

func (b *Builder) ctRecordCompilerInfo(ctx context.Context, span *trace.Span) {
	if !trace.CTEnableDebugTrace() {
		return
	}
	arg := &trace.CTCompilerInfoArg{
		Args: os.Args[1:],
		Envs: os.Environ(),
	}
	trace.CTCache(ctx, trace.CTCompilerInfo.Desc(), span, trace.CTCompilerInfo, arg)
}

func (b *Builder) ctRecordCompilerError(ctx context.Context, span *trace.Span, err, content string) {
	if !trace.CTEnableDebugTrace() {
		return
	}
	arg := &trace.CTCompilerErrorArg{
		Err:                err,
		PositionAndContent: content,
	}
	trace.CTCache(ctx, trace.CTCompilerError.Desc(), span, trace.CTCompilerError, arg)
}
