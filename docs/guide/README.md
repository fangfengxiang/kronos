# 使用指南

Kronos 的 Core 原语已可通过 Go 包使用。Assistant v1 提供一元 HTTP 和 gRPC，并可独立启用 SSE 和 go-micro v2 兼容监听。

| 我想做什么 | 当前可以查阅 |
| --- | --- |
| 判断 Kronos 是否适合我的 Go 服务 | 阅读框架的责任范围与依赖方向。[项目首页](../../README.md) · [架构概览](../architecture/overview.md) |
| 使用 Core 包 | 核对执行边界、语言选择与消息回退，运行包级单元测试和示例。[Core 首版落地设计](../design/core-v1.md) |
| 用 Proto 声明服务并提供 gRPC 和 HTTP | 确认服务定义、HTTP 映射和生成物的权威来源。[协议与传输决定](../adr/0001-protobuf-and-transport.md) |
| 发布一个可取消的 Agent 流 | 了解公开流事件、客户端取消和确定性模型装配。[Agent 流式决定](../adr/0002-agent-stream.md) · [Blades 文本 Agent 适配](../design/blades-adapter.md) |
| 为同一个服务提供中英文体验 | 分辨请求语言偏好、系统消息和模型生成内容，查看默认回退。[本地化决定](../adr/0003-localization.md) |
| 按用户时区展示时刻或归档日报 | 用 `core/timezone` 解析 IANA 时区并计算当地日期。精简部署可通过 `-tags timetzdata` 内嵌规则。[Core 时区](../design/core-timezone.md) · [可运行示例](../../core/timezone/timezone_test.go) |
| 在本机复现端到端测试 | 按纵切方案核对计划中的镜像、探针、容器退出与 CI 证据。[首条纵切](../design/first-vertical-slice.md) |

## 验证独立模块

从仓库根目录调用与 CI 相同的入口。

```sh
bash scripts/check-layer-deps.sh
bash scripts/test-modules.sh
```

这两个脚本读取 `go.work` 内登记的模块，在各自目录关闭工作区运行检查。新增封装模块时用 `go work use ./模块目录` 登记。[模块边界](../adr/0004-layer-and-module-boundaries.md)

## 生成 Assistant 协议

准备 Go 1.25.0 或更高版本，并将 [Buf 1.70.0](https://github.com/bufbuild/buf/releases/tag/v1.70.0) 放入 `PATH`。首次生成需要访问公开的 Go 模块代理和 Buf Schema Registry。生成命令从仓库根目录执行。

```sh
bash tools/proto/generate.sh
bash tools/proto/generate.sh --check
go test ./examples/assistant/...
```

修改 [Assistant Proto](../../examples/assistant/api/kronos/assistant/v1/assistant.proto) 后重新生成，提交协议与 `examples/assistant/internal/wire` 的 Go 产物。检查命令校验格式、lint 和重新生成的内容，CI 执行同一入口。[Assistant v1 协议设计](../design/assistant-protocol.md)

Assistant 的业务实现提供 `Service.Run`。调用方通过 `Run` 发起请求，由实例所有者执行 `Close`。HTTP 客户端使用 `NewHTTPClient`。RPC 客户端使用 `NewRPCClient`，默认走标准 gRPC。通过 `rpc.WithProtocol` 可选用 go-micro v2。[集成用例](../../examples/assistant/assistant_test.go) · [RPC 客户端协议](../design/rpc-client-protocol.md)

```go
standardClient, err := assistant.NewRPCClient(ctx, "127.0.0.1:9090", time.Second)
legacyClient, err := assistant.NewRPCClient(ctx, "127.0.0.1:9091", time.Second,
    rpc.WithProtocol(rpc.ProtocolMicroV2))
```

### 选择错误文案语言

HTTP 和 SSE 使用 `Accept-Language`，标准 gRPC 使用 `accept-language` 元数据。Kronos 的 HTTP 和标准 gRPC 一元客户端通过 `framework/localization` 为单次调用传入偏好。

```go
callCtx := localization.WithAcceptLanguage(ctx, "zh-CN, en;q=0.5")
response, err := client.Run(callCtx, request)
```

Assistant 的空提示词支持简体中文和英文，默认英文。调用方继续按 `ERROR_REASON_INVALID_PROMPT` 处理失败。[框架语言协商](../design/framework-localization.md)

## 运行 Assistant

Assistant 默认用 Blades 执行确定性文本模型。有效提示词在一元入口返回原文，在流式入口按顺序返回文本片段。启动无需模型账户或 API Key。[Blades 装配与验收](../design/blades-adapter.md)

```sh
go run ./examples/assistant/cmd/assistant -http 127.0.0.1:8080 -grpc 127.0.0.1:9090 -shutdown-timeout 5s
```

```sh
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/v1/assistant:run -H 'Content-Type: application/json' -d '{"prompt":"你好"}'
```

命令入口订阅退出信号，应用撤销就绪并按配置预算结束在途调用。子进程测试展示 gRPC 调用和真实 `SIGTERM` 验收。[应用装配与关停](../design/app-lifecycle.md) · [进程测试](../../examples/assistant/cmd/assistant/main_test.go)

### 同时启用 SSE 与旧协议

```sh
go run ./examples/assistant/cmd/assistant \
  -http 127.0.0.1:8080 -grpc 127.0.0.1:9090 \
  -sse 127.0.0.1:8081 -micro-v2 127.0.0.1:9091
```

```sh
curl --no-buffer -fsS http://127.0.0.1:8081/v1/assistant:streamRun \
  -H 'Content-Type: application/json' -H 'Accept: text/event-stream' -d '{"prompt":"你好"}'
bash test/interop/microv2/verify.sh 127.0.0.1:9091
```

SSE 的请求体使用 JSON。调用示例的 go-micro v2 服务名是 `kronos.assistant.v1.Assistant`，端点名是 `Assistant.Run`。互通脚本按版本构建官方客户端和服务端，首次运行需要访问 Go 模块代理。[监听与互通设计](../design/additional-listeners.md)

### 从文件读取启动配置

将监听地址和停止预算写入 `assistant.json`。键使用启动参数名，值使用字符串。

```json
{
  "http": "127.0.0.1:8080",
  "grpc": "127.0.0.1:9090",
  "sse": "127.0.0.1:8081",
  "micro-v2": "127.0.0.1:9091",
  "shutdown-timeout": "5s"
}
```

```sh
go run ./examples/assistant/cmd/assistant -config ./assistant.json -http 127.0.0.1:18080
```

显式参数覆盖文件，文件缺失的键沿用默认值。文件不存在、出现未知键或生效值无效时，入口返回启动错误。[可用配置项和检查规则](../design/config-file.md)

### 优雅重载

在 Linux 或 macOS 上，从稳定路径运行构建后的二进制。通过命令行设置 `-reload-pid-file` 启用 `SIGHUP` 接力，并设置 `-reload-timeout` 控制候选启动预算。PID 文件的上级目录由部署方准备。

Windows 上的 tableflip v1.2.3 不支持进程级重载。Windows 普通启动时省略 `-reload-pid-file`。[平台支持与验收](../design/graceful-reload.md#平台支持)

```sh
go build -o ./assistant-server ./examples/assistant/cmd/assistant
./assistant-server -reload-pid-file ./assistant.pid -reload-timeout 30s -shutdown-timeout 5s
```

在另一终端向当前 PID 发送 `SIGHUP`。候选读取同一 `-config` 路径。配置无效时原进程和 PID 文件继续使用。候选就绪后更新 PID 文件，旧进程在关停预算内完成已进入业务的请求和流。[重载契约](../design/graceful-reload.md)

```sh
kill -HUP "$(cat ./assistant.pid)"
```

最终停止时，读取当前 PID 文件并发送 `SIGTERM`。

```sh
kill -TERM "$(cat ./assistant.pid)"
```
