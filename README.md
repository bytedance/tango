<div align="center">

# Tango

[中文版](README_CN.md)

</div>

## Introduction
Tango is an enhanced distribution of the Go programming language, serving as a superset of the community‑standard Go SDK. While preserving behavior consistent with upstream Go releases, it delivers systematic enhancements to the compiler, runtime, and diagnostics.

Tango’s core principle is compatibility first. It preserves established Tango
behavior, including selected stable integrations that are enabled by default.
Other enhancements are enabled explicitly through compilation options or
runtime switches, without requiring application code changes.

At the architecture level, Tango:
- Evolves directly from the official Go source, ensuring compatibility with the standard library and language features;

### Differences from upstream Go

Tango enables selected stable changes by default. Compared with the corresponding upstream Go release, it has the following differences:

- `GOTOOLCHAIN` defaults to `local`. When a module requires a newer Go toolchain, the build fails instead of downloading one automatically. Setting `GOTOOLCHAIN=auto` may select an upstream toolchain that does not include Tango enhancements.
- Builds add the `bytedance_bversion` and `bytedance_tango` build tags and record them in build information.
- CPU profiles can record up to 128 stack frames instead of upstream Go's 64.
- `runtime.MemStats` includes `NumBalancedGC`, and `runtime/metrics` includes `/gc/gcadaptive/throughput:percent`.
- PGO preprocessing records a recommended initial goroutine stack size based on the profile's existing stack size and `runtime.copystack` CPU cost. With `pgostacksize=1`, Tango adjusts the runtime's initial stack size accordingly to reduce stack-growth copying overhead; the corresponding upstream Go compiler does not recognize this Tango-specific profile format.
- Importing `net/http/pprof` also registers Tango's Dynconf endpoint.

## Features Introduction
Tango offers enhancements grouped into four categories—compiler, runtime,
diagnostics, and standard library & APIs. The following is a brief feature
list.

### Compiler Enhancements
- Beast Compilation Mode: a compiler strategy tailored for backend, long‑running services, with the primary goal of generating high‑performance machine code.
- PGO (Profile‑Guided Optimization) Enhancements: uses runtime performance sampling data to guide compiler optimization strategies—such as hot‑function inlining, basic‑block layout, function placement, and conditional branch optimization—and further strengthens strategies in recursive and stack‑growth scenarios.

### Runtime Enhancements
- Balanced GC: allocates small objects using bump‑pointer on the per‑G allocation buffer and manages that memory via a moving‑object GC, improving object allocation and reclamation efficiency.

### Diagnostics & Observability
- pprof Enhancements: enhances pprof to expose details of Go heap allocation and reclamation, and allows dynamically viewing and modifying select Go runtime configurations while the program is running—without restart or code changes.
- LargeAllocTrace: records call stacks and object sizes for large memory allocations, providing auxiliary diagnostics for issues such as OOM.

### Standard Library & API Extensions
- tango/conv: provides efficient and safe utilities for common type‑conversion scenarios.

## Using Tango

### Build from Source
You can build the Tango toolchain from source the same way as standard Go.

Clone the repository
```bash
git clone https://github.com/bytedance/tango.git tango
cd tango
```

Build the toolchain
Enter the src directory and run the build script. This builds the complete Go distribution (compiler, standard library, and toolchain).
```bash
cd src
./make.bash
```

Configure environment variables
After the build completes, the new go command resides at ../bin/go. You can add that directory to PATH to use it directly:
```bash
export PATH="$(pwd)/../bin:$PATH"
```
Then run go version as well as go bversion to confirm you are using the Tango‑built Go distribution.

### Enable Enhanced Features Examples
Most optional enhancements must be enabled explicitly. The examples below show
how to enable several common features via compile-time options or runtime
environment variables (assuming Tango’s go is already on PATH).

Beast Compilation Mode (compile‑time option): add -opt=beast to the build command to enable Beast mode.
On linux/amd64, Beast mode raises `GOAMD64` to at least `v3`. Binaries  built in this mode require a processor that supports `GOAMD64=v3` and may not run on older x86-64 processors.
```bash
go build -opt=beast -o my_app main.go
```

PGO Enhancements (compile‑time option): use a collected CPU profile file via -pgo perf.pb.gz during compilation.
```bash
go build -pgo perf.pb.gz -gcflags=all='-d=pgorecursive=1,pgostacksize=1' -o my_app main.go
```

Balanced GC (runtime option): enable Balanced GC by setting GODEBUG=gab=2.
```bash
export GODEBUG=gab=2
```

LargeAllocTrace (runtime option): before startup, set GODEBUG=largealloctrace=104857600 to record call stacks for individual allocations not smaller than 100MB.
```bash
export GODEBUG=largealloctrace=104857600
```

Dynconf (runtime option): importing `net/http/pprof` registers the
`/debug/pprof/dynconf` HTTP interface by default to preserve established Tango
behavior. The interface can query or modify certain settings at runtime. Apply
the same access controls to it as to the other pprof endpoints.
```bash
curl -d 'cmd=list' http://127.0.0.1:8080/debug/pprof/dynconf
curl -d 'cmd=set&args=runtime.gctrace=1' http://127.0.0.1:8080/debug/pprof/dynconf
```

## Contribution
Tango welcomes feedback and contributions from the community, including but not limited to: issue reports, performance and compatibility regressions, documentation improvements, and new feature proposals.

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

## License
Unless otherwise noted, ByteDance contributions are distributed under the
BSD 3-Clause license in
[LICENSE](LICENSE). Tango is based on [Go](https://github.com/golang/go); its
original BSD 3-Clause license is preserved in [LICENSE_UPSTREAM](LICENSE_UPSTREAM), and
[PATENTS](PATENTS) is retained unchanged. Upstream and third-party code retains
its original license. See [NOTICE](NOTICE) for sources and license locations.

## Acknowledgements
Tango is built upon the Go language and its ecosystem, benefiting from the community’s long‑standing accumulation of excellent designs and implementations. We extend our sincere thanks to the Go team and all developers who contribute to the Go ecosystem, and we look forward to continuing to refine this distribution together with the broader open‑source community.

For the original Go README, see [README_UPSTREAM.md](README_UPSTREAM.md).
