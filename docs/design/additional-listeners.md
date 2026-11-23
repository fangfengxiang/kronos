# SSE 与 go-micro v2 监听

## 使用者与装配

Assistant 应用同时支持现有 gRPC 和 HTTP，以及按地址显式启用的 SSE 和 go-micro v2 监听。四种监听沿用 `app.Binding`，进入同一次资源取得、就绪、摘流和退出。业务实现接收标准上下文与 Protobuf 类型。Kratos 负责 HTTP、SSE 和 gRPC 运行，旧协议在独立的 `infra/microv2` 模块解析与转换。

## SSE 契约

- `StreamRun` 同时提供 gRPC 服务端流与 Proto 显式绑定的 SSE `POST /v1/assistant:streamRun`。提示词来自 JSON 请求体。浏览器使用 Fetch 流读取。原有 HTTP 监听提供一元 `Run`，SSE 监听提供 `StreamRun`。
- 业务回调只产生文本增量。适配层成功时发送一条 `completed` 消息，随后结束。SSE 数据使用 `event: message` 加 Proto JSON。首帧前的异常使用 HTTP 错误，已经发送正文后使用 `event: error`，结束时缺少完成标记视为不完整。流消费依靠正常传输结束和完成标记联合判定。
- 直接复用 Kratos 的 SSE 生成绑定和事件编码。Kratos v3.0.0 的 HTTP stream 会分离原请求取消，私有适配带回 TCP 请求上下文，使断开能终止正在等待的业务。单次事件写入使用 15 秒的写期限，在途 SSE 共享 `App` 的停止预算。[Kratos SSE 实现](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/http/stream.go) · [SSE 格式](https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream)

## go-micro v2 兼容范围

- 官方 go-micro v2.0.0、v2.9.1 默认 RPC 通过 HTTP `POST /` 携带目标服务、方法、调用 ID、纳秒级 `Timeout` 与裸 Protobuf；旧 `application/proto-rpc` 使用两个大端长度帧，分别承载 Protobuf 信封和业务消息。参考应用的目标服务为 `kronos.assistant.v1.Assistant`，方法为 `Assistant.Run`。
- 已识别的一元调用用 HTTP 200 返回 RPC 响应。远端业务错误沿 `Micro-Error` 及旧帧错误字段传输，公开 `id`、`code`、`detail` 和 `status`。请求体限制为 1 MiB。交给用例的上下文期限最长为 30 秒，较短的客户端期限继续生效。错误帧保留调用序号。旧版编解码器可先按不可信长度分配内存，适配先限定输入，再以实际数据长度验证两帧。旧 SDK 只用于独立互通验收。[v2 HTTP 传输](https://github.com/micro/go-micro/blob/v2.9.1/transport/http_transport.go) · [旧信封定义](https://github.com/micro/go-micro/blob/v2.9.1/codec/protorpc/envelope.proto)
- `application/proto-rpc` 的收发双方按完整响应体限制 1 MiB，计入两个长度字段、Protobuf 信封和业务消息。正好 1 MiB 可以正常返回。成功负载或错误信封超限时，服务端仍返回 HTTP 200，使用保留调用序号的紧凑错误帧和 `Micro-Error`，原因 `RESPONSE_TOO_LARGE`、RPC 错误码 500。Assistant 公开客户端将该故障映射为已有的 `ERROR_REASON_RUN_FAILED`。裸 `application/protobuf` 沿用原响应行为。
- 首版兼容上述两种一元 Protobuf 编码与直连监听。服务发现、发布订阅、旧版流及业务方私有协议变体需要独立的协议样本与验收。

## 验收

- 同进程开启四类监听，核对原有 HTTP、标准 gRPC、SSE 和两种 go-micro v2 编码。SSE 检查增量、完成、中途失败与断连取消。两版真实旧客户端分别从独立模块调用并验证成功及业务错误。
- 保留协议生成一致性、各独立模块竞态、跨层导入和应用退出检查。测试旧帧长度造假、请求过大、未知方法和超时。
- 验证完整响应体恰好达到上限时成功，超过一个字节或错误字段超限时收到可解码的受控 RPC 错误。两版官方客户端验证有帧编码超限及裸编码大响应兼容。

## 实测与复盘

| 首次验收项目 | Go 1.25.0 实测结果 |
| --- | --- |
| 模块竞态和进程测试 | 各模块竞态测试通过。Assistant 子进程四类监听均可调用，`SIGTERM` 后以状态码 0 退出，四个端口均可重新绑定。SSE 覆盖请求体、失败时机和断连取消。 |
| 两版 go-micro v2 互通 | 两版官方客户端通过默认连接池连接同一个四端口 Assistant 进程。每版均通过 `application/protobuf` 和 `application/proto-rpc` 验证 Unicode 响应及 `ERROR_REASON_INVALID_PROMPT`。实测兼容地址由回环测试分配。 |
| 分层和生成检查、`GOWORK=off go vet ./...`、`GOWORK=off go mod verify` | 跨层导入、协议生成一致性、静态检查和模块完整性检查通过。 |

互通夹具保留两版客户端源码，单独提升 `golang.org/x/net` 以适配现代 Go 构建，并从 v2.0.0 的临时模块清单移除客户端未引用的 v1 模块。该 v1 模块依赖已下线的代码生成工具。当前使用 `go test -tags=interop -count=1 ./test/interop/microv2` 验证两个方向。[原生 Go 互通验收](go-micro-v2-interop.md)

修复前，超过一字节的成功响应和超长错误均以 HTTP 200 写出超限帧。Go 1.25.0、Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C infra/microv2 test -count=3 ./...` 通过，macOS arm64 同项以 `-race` 三轮通过。Linux 两版官方互通通过大响应检查。[尺寸回归](../../infra/microv2/handler_test.go) · [双向互通实测](go-micro-v2-interop.md#实测与复盘)
