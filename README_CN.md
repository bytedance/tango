<div align="center">

# Tango

[English](README.md)

</div>

## 简介
Tango 是一个增强版 Go 语言发行版，作为社区标准 Go SDK 的超集。在保持与上游 Go 版本行为一致的前提下，对编译器、运行时和诊断能力进行了系统性增强。
Tango 的核心原则是 **兼容优先**。Tango 会保留已有的对外行为，包括默认启用部分经过验证的稳定集成；其他增强能力通过编译选项或运行时开关显式启用，无需修改业务代码。
在架构层面，Tango：
- 基于官方 Go 源码演进，保证对标准库和语言特性的兼容性；
- 提供基于 LLVM 的可选编译期后端，需使用准备后的 TangoLLVM 工具链，设置 `GOEXPERIMENT=tangollvm` 并传入 `-gcflags=all=-beast=llvm=N`（N≥2）才能启用；默认仍使用官方 Go 编译器后端，编译产物在运行时不依赖 LLVM。

### 与上游 Go 的默认行为差异

Tango 默认开启了部分稳定改动。与对应版本的上游 Go 对比存在以下差异：

- `GOTOOLCHAIN` 默认为 `local`。当模块要求更新的 Go 工具链时，构建会直接失败，不会自动下载工具链。设置 `GOTOOLCHAIN=auto` 可能会切换到不包含 Tango 增强的上游工具链。
- 构建时默认添加 `bytedance_bversion` 和 `bytedance_tango` build tag，并将其记录到构建信息中。
- CPU profile 最多记录调用栈层数由 64 层修改为 128 层。
- `runtime.MemStats` 增加了 `NumBalancedGC` 字段，`runtime/metrics` 增加了 `/gc/gcadaptive/throughput:percent` 指标。
- PGO 预处理会根据 profile 中已有的初始栈大小和 `runtime.copystack` CPU 开销，写入建议采用的 goroutine 初始栈大小；开启 `pgostacksize=1` 后，Tango 据此调整 runtime 的初始栈大小，以减少栈扩容时的复制开销。对应版本的上游 Go 无法识别该专用格式。
- 导入 `net/http/pprof` 时还会注册 Tango 的 Dynconf 接口。

## 特性介绍
Tango 按编译器、运行时、诊断以及标准库与 API 四类提供增强特性，下面以特性列表形式做简要概览。
### 编译器增强
- **Beast 编译模式**：针对性优化后端、长期执行服务的编译器策略，生成高性能机器码。
- **PGO（Profile-Guided Optimization）增强**：采用运行期的性能采样数据指导编译器调整优化策略，如热点函数内联、基本块布局、函数布局、条件分支优化等，进一步增强了 recursive 以及 栈扩容场景下的优化策略。
- **可选 LLVM 后端**：在默认 Go 编译器后端之外提供基于 LLVM 的可选编译期后端，用于探索不同的代码生成和优化策略。
### 运行时增强
- **Balanced GC**：将小对象通过 bump pointer 的方式在 per-g allocation buffer 上分配，并使用移动对象 GC 管理这部分内存，提高对象分配和回收效率。
### 诊断与可观测性
- **pprof 增强**：增强 pprof 能力，展示 Go 堆内存分配与回收细节以及在程序运行时可动态查看和修改部分 Go runtime 配置，无需重启或改代码即可生效。
- **大内存分配追踪（LargeAllocTrace）**：在大内存申请时记录调用栈和对象大小信息，为 OOM 等问题提供辅助定位手段。
### 标准库与 API 扩展
- **tango/conv**：提供更高效与安全的常用类型转换工具。

## 使用 Tango
### 从源码构建
你可以像构建标准 Go 一样，从源码构建 Tango 工具链。
1. **克隆仓库**
```bash
git clone https://github.com/bytedance/tango.git tango
cd tango
```
2. **构建工具链**
进入 `src` 目录并运行构建脚本，这将构建完整的 Go 发行版（编译器、标准库和工具链）。
```bash
cd src
./make.bash
```
3. **配置环境变量**
构建完成后，新生成的 `go` 命令位于 `../bin/go`。你可以将该目录加入 `PATH` 以便直接使用：
```bash
export PATH="$(pwd)/../bin:$PATH"
```
之后执行 `go version` 以及  `go bversion`  即可确认当前正在使用 Tango 构建的 Go 发行版。

### 启用增强特性示例
大部分可选增强能力需要显式开启。以下示例展示了如何通过编译期选项或运行时环境变量启用部分常见特性（假设已经将 Tango 的 `go` 命令加入 `PATH`）。
- **Beast 编译模式**（编译期选项）：在编译命令中添加 `-opt=beast` 开启 Beast 模式。 在 linux/amd64 上，Beast 模式会将 `GOAMD64` 最低设置为 `v3`。使用该模式构建的二进制要求处理器支持 `GOAMD64=v3`，可能无法在较旧的 x86-64 处理器上运行。
```bash
go build -opt=beast -o my_app main.go
```
- **PGO 增强**（编译期选项）：使用采集好的 CPU profile 文件，通过 `-pgo perf.pb.gz` 参与编译。
```bash
go build -pgo perf.pb.gz -gcflags=all='-d=pgorecursive=1,pgostacksize=1' -o my_app main.go
```
- **LLVM 编译后端（实验性）**：先按照 [TangoLLVM 构建说明](doc/tangollvm.md)，
使用 [TangoLLVM 仓库](https://github.com/bytedance/tangollvm) 构建匹配的工具链。普通 `make.bash` 构建不包含
准备后的 LLVM 绑定和 SDK。随后使用生成的工具链：
```bash
env -u GOROOT GOEXPERIMENT=tangollvm \
  /path/to/tango-llvm-build/bin/go build \
  -gcflags='all=-beast=llvm=3,optimize=3,inline=1' -o my_app_llvm main.go
```
- **Balanced GC**（运行期选项）：通过设置 `GODEBUG=gab=2` 启用 Balanced GC。
```bash
export GODEBUG=gab=2
```
- **大内存分配追踪（LargeAllocTrace）**：在启动前设置 `GODEBUG=largealloctrace=104857600`，记录单次分配不小于 100MB 的调用栈。
```bash
export GODEBUG=largealloctrace=104857600
```
- **运行时动态配置（Dynconf）**：为了保持 Tango 已有行为，导入 `net/http/pprof` 时会默认注册 `/debug/pprof/dynconf` HTTP 接口，用于在运行时查询或修改部分配置。请为该接口配置与其他 pprof 接口相同的访问控制。
```bash
curl -d 'cmd=list' http://127.0.0.1:8080/debug/pprof/dynconf
curl -d 'cmd=set&args=runtime.gctrace=1' http://127.0.0.1:8080/debug/pprof/dynconf
```

## 贡献
Tango 欢迎来自社区的反馈与贡献，包括但不限于：问题报告、性能与兼容性回归、文档改进以及新特性提案。
贡献指南见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可证
除非文件另有说明，ByteDance 贡献的代码采用 [LICENSE](LICENSE) 中的 BSD 3-Clause 许可证。Tango 基于 [Go](https://github.com/golang/go) 开发，上游原始 BSD 3-Clause 许可证保留在 [LICENSE_UPSTREAM](LICENSE_UPSTREAM)，[PATENTS](PATENTS) 保持不变。上游及第三方代码保留各自原有许可，来源和许可证位置见 [NOTICE](NOTICE)。

## 致谢
Tango 构建在 Go 语言及其生态之上，受益于社区长期积累的大量优秀设计与实现。我们对 Go 团队以及所有为 Go 生态做出贡献的开发者表示诚挚感谢，也期待与更广泛的开源社区一起持续打磨这一发行版。

Go 的原始 README 保留在 [README_UPSTREAM.md](README_UPSTREAM.md)。
