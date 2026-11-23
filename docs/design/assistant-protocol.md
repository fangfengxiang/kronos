# Assistant v1 协议设计

## 使用者与入口

HTTP JSON 客户端和 gRPC 客户端从同一份版本化 Protobuf 获取方法、输入、输出和错误标识。参考应用拥有协议，生成工具依赖协议，框架负责把生成的入口接到同一个用例。

| 方法 | 入口 | 请求与响应 |
| --- | --- | --- |
| `Run` | gRPC 一元，HTTP `POST /v1/assistant:run` | 请求包含 `prompt`，响应包含完整 `text`。HTTP 从 JSON 请求体读取输入。 |
| `StreamRun` | gRPC 服务端流，SSE `POST /v1/assistant:streamRun` | 请求包含 `prompt`，响应按发送顺序交付文本增量与完成标记。 |

两种方法各自拥有请求消息，首版采用同一条校验规则。空字符串或仅含空白的提示词为无效请求。原始提示词交给应用用例。系统文案语言从传输入口协商。Agent 输出语言由请求意图与用例决定。

## 流与错误

- `StreamRunResponse.event` 使用 `oneof`。文本增量承载新增正文，客户端按顺序拼接。完成标记无需正文。成功流发送恰好一个完成标记，随后以 gRPC OK 结束。客户端观察到完成标记和正常 EOF 后确认成功。
- 上游失败时，gRPC 以非 OK 状态结束流。SSE 在首帧前返回 HTTP 错误，正文开始后发送 `event: error`。客户端可以保留已接收的文本作为部分输出。缺少完成标记或缺少正常传输结束的流视为未完成。取消传播至执行端。
- 服务使用枚举值名作为稳定错误标识。框架接入时映射到 HTTP 响应的 `reason` 和 gRPC `google.rpc.ErrorInfo.reason`。错误消息可按请求语言渲染。

| 错误标识 | 场景 | HTTP | gRPC |
| --- | --- | --- | --- |
| `ERROR_REASON_INVALID_PROMPT` | 提示词为空或全空白 | 400 | `InvalidArgument` |
| `ERROR_REASON_RUN_FAILED` | Agent 执行失败 | 500 | `Internal` |

## 取舍与依赖

- 用生成的 Kratos HTTP 绑定直接调用服务实现，HTTP 与 SSE 入口来自显式注解。SSE 的事件格式、断线规则及独立监听见 [SSE 与 go-micro v2 监听](additional-listeners.md)。[Kratos 传输](https://go-kratos.dev/docs/component/transport/overview/)
- 使用完成标记和传输状态分别表达应用完成与交付结束。新增事件需评估旧客户端对未知事件的处理。维护已发布的字段编号与枚举含义，删除字段时保留其名称与编号。[Protobuf 协议演进](https://protobuf.dev/programming-guides/proto3/#updating) · [gRPC 生命周期](https://grpc.io/docs/what-is-grpc/core-concepts/)
- 用 Buf 和锁定的 Google API 注解编译 Schema。固定 Go、gRPC 和 Kratos HTTP 生成插件的版本，随协议提交生成产物。稳定错误标识由普通 Protobuf 枚举生成，传输映射由框架交付。[Buf 生成配置](https://buf.build/docs/configuration/v2/buf-gen-yaml/) · [Kratos 错误](https://go-kratos.dev/docs/component/errors/)

## 验收

- 检查 Schema 命名、版本目录和格式。用描述符验证一元路由、SSE 路由、请求体来源和流类型。
- 用生成的消息验证两类事件可通过 Protobuf 编码往返，并锁定公开错误标识及其编号。用可编译的生成客户端和注册接口验证三类 Go 产物。
- 将生成产物写入临时目录，与仓库逐文件比较。CI 使用同一命令。双入口错误映射、流的取消与终止顺序在真实监听的应用切面验收。

当前从仓库根目录运行 `go run ./platform/verify proto`，需要更新协议产物时运行 `go run ./platform/verify generate`。同一生成命令提供公开消息和服务接口以及私有传输绑定。[原生 Go 验收](go-verification.md) · [公开契约生成](protobuf-go-contract.md)

## 实测记录

Go 1.25.0、Buf 1.70.0、macOS arm64 环境完成以下检查。

| 首次验收项目 | 结果 |
| --- | --- |
| 协议生成一致性 | 格式与 lint 通过，三份 Go 产物与重新生成结果一致。向隔离副本的 HTTP 产物追加内容后，检查返回失败并报告差异。 |
| `go test -race -count=1 ./...` | Core 及协议测试通过。生成的 Kratos 绑定仅注册预期路由，JSON 请求体中的提示词在同名查询参数存在时仍正确传入。 |
| `go vet ./...`、`go mod verify`、分层检查 | 静态检查、依赖校验和跨层依赖检查通过。 |

描述符测试固定公开入口，路由测试覆盖生成器与运行库的实际组合。应用切面继续验证真实监听上的双入口响应、稳定错误、本地化、流取消与资源收尾。
