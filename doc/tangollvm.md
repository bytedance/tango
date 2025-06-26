# Building TangoLLVM

The normal Tango toolchain builds without LLVM. Tango retains its own compiler
and linker integration flow and maintains the Go-to-LLVM IR translator
(`src/cmd/compile/internal/ssagen/llvm.go`), its tests, object handling,
isolation switches and stubs. Parts of the translator were originally adapted
from TinyGo v0.25.0's `compiler/compiler.go` and were subsequently integrated
with the Go compiler's internal IR, SSA, type and ABI representations. See
[`LICENSE-TinyGo`](../src/cmd/compile/internal/ssagen/LICENSE-TinyGo) for its
license. The translator is excluded from normal builds by its build
constraints. LLVM bindings and the generated C bridge are maintained in the
[TangoLLVM repository](https://github.com/bytedance/tangollvm) and copied in
only for an LLVM build.

Build the customized SDK in that repository with `./build.sh`, then run:

```sh
TANGOLLVM_REPO=/path/to/tangollvm \
TANGOLLVM_BUILD_ROOT=/path/to/new-tango-llvm-build \
GOROOT_BOOTSTRAP=/path/to/bootstrap-go \
./src/make.tangollvm.bash
```

This requires native Linux/amd64, Git, Bash 4.3 or newer, GNU coreutils,
GNU tar, findutils, and a C compiler.
`TANGOLLVM_BUILD_ROOT` must be a new directory outside both source repositories.
The LLVM script copies Tango into it, checks overlay destinations,
copies the LLVM-owned bindings and SDK, and builds
there.
It does not change either source checkout or replace an existing output
folder. The generated compiler is `TANGOLLVM_BUILD_ROOT/bin/go`.

Use that toolchain to build an application. Set `GOEXPERIMENT=tangollvm` and
pass `-gcflags=all=-beast=llvm=N` with N≥2; setting the experiment alone
does not enable LLVM code generation:

```sh
env -u GOROOT GOEXPERIMENT=tangollvm \
  /path/to/new-tango-llvm-build/bin/go build \
  -gcflags='all=-beast=llvm=3,optimize=3,inline=1' ./...
```

The SDK defaults to the generated toolchain's `pkg/tangollvm` directory.
`GOEXPERIMENT=notangollvm` selects its standard compiler and linker.
Plain Tango continues to build with `src/make.bash` without an LLVM checkout.

Run the existing TangoLLVM regression suite from the generated directory:

```sh
cd /path/to/new-tango-llvm-build
env -u GOROOT ./test/llvm/test.sh 3 3 1
```

The runner builds and executes the standalone samples and the `linkname`
and `abi` directory tests. Directory tests include their assembly files;
`linkname` builds in its own module. Any build or execution failure makes
the runner fail. Binaries and the build cache are isolated per run.

See `tango/README.md` in the matching
[TangoLLVM repository](https://github.com/bytedance/tangollvm) checkout for
ownership boundaries, the copy manifest, source provenance, and validation
commands.

Maintain the translator with Tango's SSA, type and ABI changes. Changes to
custom LLVM intrinsics or metadata contracts must also update the paired
TangoLLVM backend or bindings. Validate each Tango branch with a toolchain build and the
regression tests; preparation does not enforce interface file hashes. Actual
source revisions and hashes are recorded in `tangollvm-source.json`. The
translator is copied from Tango, not replaced by an overlay.

## Standard library tests with TangoLLVM

Use `go tool dist test -tl` to pass additional compiler flags to the LLVM
standard library test mode. `-env NAME=value` sets an environment variable for
that mode; repeat `-env` for multiple variables. Values may contain colons,
spaces, or equals signs. Both flags apply only to `tangollvm_test:` tests.
Select both the standard package and its LLVM test variant, as below.

```sh
cd /path/to/new-tango-llvm-build
env -u GOROOT GOEXPERIMENT=tangollvm ./bin/go tool dist test \
  -run='^(fmt|tangollvm_test:fmt)$' -tl='-beast=llvm=3,optimize=3,inline=1' \
  -env GOGC=20
```

## Manual test scripts

From a prepared TangoLLVM toolchain, run `bash ./test.sh` to execute both the
upstream testdir suite and the TangoLLVM samples with LLVM mode 3, optimization
level 3, and inlining level 1. The script exits nonzero if either suite fails.
It is a manual entry point and is not invoked by `all.bash` or `cmd/dist`.

To run only the upstream testdir suite, use `bash ./test/test.sh 3 3 1`.
The one to three positional arguments select LLVM mode, optimization level,
and inlining level, respectively. It writes its summary to `test/failures.txt`
and uses a temporary build cache. Each selected file must produce its own
test result; skipped cases are reported separately. The standalone `stress`
programs are not registered with testdir and are excluded from this script.

## Runtime Duff assembly

Tango owns `src/runtime/duff_amd64.s` and its generator, `src/runtime/mkduff.go`.
The assembly is enabled only by `goexperiment.tangollvm`; normal builds exclude
it. Regenerate it from `src/runtime` with `go run mkduff.go`. Changes to the
Duff instruction layout must stay aligned with LLVM's Duff entry offsets.
