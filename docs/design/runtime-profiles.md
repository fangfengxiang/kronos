# 按需运行时快照

Assistant 运维人员可以在与服务相同的主机或容器网络空间内检查 Go 堆采样和 goroutine 调用栈。入口用 `-runtime-profile 127.0.0.1:6060` 或启动 JSON 启用，默认关闭。

## 责任与契约

- `infra/runtimeprofile` 是独立模块。`NewServer(net.Listener)` 接收已取得的 TCP 监听，验证实际监听地址是 IPv4 或 IPv6 loopback。校验失败由调用方负责关闭监听。成功返回的实例拥有 `Start(context.Context)` 与 `Stop(context.Context)`，可以由 `framework/app` 管理。
- `GET /debug/pprof/heap` 和 `GET /debug/pprof/goroutine` 返回各自的 gzip Protobuf 快照，可用 `go tool pprof` 读取。每次请求即时生成快照。两条路径的其他方法返回 `405`，其余路径返回 `404`。采用固定的采样格式，查询参数无需参与采样。
- Assistant 放在独立监听上装配此组件，监听与业务端口一起完成启动、回滚、SIGHUP 交接和关停。地址校验或监听失败时进程启动失败。应用协议、请求指标和链路追踪沿用现有业务入口。

## 复用与取舍

参考实现由进程启动 Holmes，按资源阈值采样并负责报告与落盘。当前运维动作是获取正在运行的 Assistant 现场快照。沿用 `runtime/pprof` 生成样本，用现有 Kratos HTTP server 管理监听生命周期。自动阈值采集、报告和留存依赖单独的运行约定。[Go runtime/pprof](https://pkg.go.dev/runtime/pprof@go1.25.0#Profile.WriteTo) · [Holmes](https://github.com/mosn/holmes)

Go 的 `net/http/pprof` 提供完整的诊断路由并在导入时注册全局 mux。当前适配仅注册两条固定路径，把 Kratos 的未匹配路由显式指向 `404` 处理器。样本沿用 Go 1.25.0 的 writer，响应携带 `Cache-Control: no-store`。Kratos v3.0.0 沿用已有依赖版本。[Go net/http/pprof](https://pkg.go.dev/net/http/pprof@go1.25.0) · [Kratos HTTP server v3.0.0](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/http/server.go)

## 验收条件

- 真实 loopback HTTP 可下载两种快照，Go 工具可解析，样本类型与 Go 标准 HTTP handler 一致。拒绝 wildcard 地址、业务路由和非 GET 方法。
- Assistant 子进程用启动 JSON 启用诊断，业务 HTTP 路由与诊断路由各自隔离。收到 `SIGTERM` 后进程正常退出，监听端口可重新绑定。
- 借助真实 SIGHUP 进程交接确认已配置的监听能继续采样。在 Linux 环境执行包和应用入口测试，并记录实测平台。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 上运行 `GOWORK=off go -C infra/runtimeprofile test -count=3 -v ./...`。IPv4 与 IPv6 回环均可取样。对照 `net/http/pprof`，`go tool pprof -raw` 在两种实现的输出中均识别出 heap 的 `inuse_space/bytes` 和 goroutine 的 `goroutine/count`。确认全局 mux 已注册的命令行与 CPU 路由在独立服务上返回 `404`。[原生对照测试](../../infra/runtimeprofile/server_test.go)
- 在同一 Linux 环境运行 `GOWORK=off go test -count=2 -run 'TestAssistant(ProcessShutsDownOnSIGTERM|ReloadKeepsAllListeners|RejectsWildcardRuntimeProfiles)$' ./examples/assistant/cmd/assistant`。真实进程启动后可采样，候选失败后旧进程仍可采样，接力后新进程继续服务同一地址，最终 `SIGTERM` 释放五个监听。wildcard 地址会使装配失败并释放先前取得的端口。[启动和关停测试](../../examples/assistant/cmd/assistant/main_test.go) · [进程接力测试](../../examples/assistant/cmd/assistant/reload_test.go)
- macOS arm64 上 `go run ./platform/verify test` 与 `go run ./platform/verify deps` 通过。从宿主启动的 Linux Compose 端到端测试也通过，默认部署使用四个业务端口。诊断模块与 Assistant 通过 Windows amd64 交叉构建，运行实测平台为 Linux 和 macOS。[进程重载平台支持](graceful-reload.md#平台支持)
- 使用 Go overlay 暂时移除 Kratos 的独立 `404` 处理器，`TestProfileRoutesAreIsolated` 观察到全局索引与 `cmdline` 返回 `200`，验证回归用例能识别全局 mux 回退。[路由隔离测试](../../infra/runtimeprofile/server_test.go)
