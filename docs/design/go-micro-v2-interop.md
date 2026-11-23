# go-micro v2 原生 Go 互通验收

## 消费者与契约

兼容协议维护者在仓库根目录运行 `go test -tags=interop -count=1 ./test/interop/microv2`。测试启动回环地址上的 Assistant 兼容绑定，为每个固定版本单独构建上游夹具，并验证两个方向的调用。

| 方向 | 验收行为 |
| --- | --- |
| 官方客户端 → Kronos | 用 `v2.0.0` 和 `v2.9.1` 分别以两种编码验证 Unicode 响应和空提示词错误。裸 Protobuf 验证读取大响应，有帧编码验证可解码的超限 RPC 错误。 |
| Kronos 客户端 → 官方服务端 | 使用公开的 `rpc.WithProtocol(rpc.ProtocolMicroV2)` 选择兼容传输，验证 Unicode 响应及稳定的 `ERROR_REASON_INVALID_PROMPT`。 |

夹具由 Go 测试管理构建、就绪、超时及释放。上游服务端通过 stdin EOF 收到结束请求。构建或调用失败时，对应子测试输出版本、阶段和进程证据。首次构建需要访问公共 Go 模块代理或等效缓存。

## 边界与取舍

- 参考类库使用具体的 RPC 客户端连接本地测试服务，并以 Go `testing` 验证直接调用与错误。Kronos 沿用真实回环入口与公开客户端，复用仓库已有上游请求和服务端夹具。两个官方版本的依赖分别锁在临时模块中。[Go 子进程与管道](https://pkg.go.dev/os/exec@go1.25.0#Cmd.StdinPipe) · [go-micro v2.9.1 模块](https://github.com/micro/go-micro/tree/v2.9.1)
- 夹具继续使用 `golang.org/x/net v0.54.0` 适配 Go 1.25。旧版 `v2.0.0` 的未使用 v1 依赖指向已下线的生成工具，测试只在该版本的临时源码副本移除依赖。诊断和产物在测试临时目录内。
- `interop` 标签控制需要旧依赖和额外进程的对照验收。日常兼容模块与 Assistant 进程测试在普通 `go test` 下运行。

## 通过条件

- Linux 上运行两版原生 Go 对照测试，通过四种官方客户端组合和两版官方服务端调用，服务和子进程均正常释放。
- 原有监听及错误转换测试通过。使用文档通过 Go 命令复现互通并链接公开协议选项的可执行示例。

## 实测与复盘

- Go 1.25.0、Ubuntu 24.04 Linux arm64 上运行 `GOWORK=off go test -tags=interop -count=1 -v ./test/interop/microv2`。两个版本都通过官方客户端到 Kronos 的两种 MIME 请求及 Kronos 到官方服务端的请求。成功返回 `你好`，失败返回稳定的提示词错误。测试收到两个官方服务端正常退出结果。[Go 互通测试](../../test/interop/microv2/interop_test.go)
- macOS arm64 上运行 `GOWORK=off go test -race -tags=interop -count=1 -v ./test/interop/microv2`，两版通过。复核将临时构建目录、进程输入、就绪时间和回收时间交给同一个子测试管理。`go -C framework/rpc test -count=1 ./...` 在 Linux 和 macOS 通过，并执行协议选项示例。[官方夹具生命周期](../../test/interop/microv2/fixture_test.go) · [Go 选项示例](../../framework/rpc/example_test.go)
- 在 Ubuntu 24.04 Linux arm64 使用 Go 1.25.0 复跑 `GOWORK=off go test -tags=interop -count=1 -v ./test/interop/microv2`。两版官方客户端均可读取裸 Protobuf 大响应，并在有帧请求解码 `RESPONSE_TOO_LARGE`。Kronos 的公开客户端收到 `ERROR_REASON_RUN_FAILED`，双向 Unicode 与业务错误回归通过。[大响应互通](../../test/interop/microv2/interop_test.go)
