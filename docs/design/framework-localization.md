# 框架语言协商

## 使用者与最小契约

Assistant 的调用方可以选择错误文案语言。HTTP、SSE 和 MicroRPC 从 `Accept-Language` 读取候选。gRPC 从 `accept-language` 元数据读取同样格式的候选。Kronos 的 HTTP、标准 gRPC 和 MicroRPC 一元客户端可通过 `localization.WithAcceptLanguage(ctx, values...)` 为当前调用传出偏好。

`framework/localization` 持有上下文中的偏好快照，使用同一套权重规则组装有序标签。入站偏好用于当前请求的语言选择，出站偏好由调用方显式设置。`Negotiate` 将候选交给调用方提供的窄 `Matcher` 接口，返回已注册语言以及是否命中偏好。Assistant 装配 `core/locale.Matcher` 和只读消息目录，业务只接收标准上下文和 Kronos 接口。Kratos 的头字段类型留在应用私有绑定。

统一一元入口的两个传输也使用此契约。生成的客户端接收同一个 `WithAcceptLanguage` 上下文，由私有传输把候选写入 gRPC 元数据或 MicroRPC 的 HTTP 头。服务端把实际收到的字段写入入站上下文，因而业务只需调用 `Negotiate`。入站偏好留在当前请求，后续调用通过 `WithAcceptLanguage` 明确设置。缺少字段时按该服务的默认语言处理。旧协议适配器保有编解码与已有调用签名，可选元数据只开放语言与现有 Trace 字段。

## 顺序与回退

- 合并同一次请求的多个字段值，按质量权重降序。权重相同保留字段和候选的原始顺序。无效项和 `q=0` 项跳过。通配符参与默认回退，具体语言优先。
- 缺少偏好或无法匹配支持语言时使用服务默认语言。Assistant 支持 `en` 和 `zh-Hans`，默认 `en`。`zh-CN` 可匹配 `zh-Hans`。缺少译文由目录回退到默认资源。
- 首个消费者按稳定标识 `ERROR_REASON_INVALID_PROMPT` 选择空提示词文案。HTTP、标准 gRPC、SSE 和 MicroRPC 使用各自的传输状态及同一稳定标识。文本增量和 Agent 输出语言依照应用契约。

## 依赖与取舍

参考调用在已确定语言后从应用字典取文案。Kronos 复用这个资源归属方式，复用仓库已有版本的 `golang.org/x/text/language.ParseAcceptLanguage` 解析候选，权重并列顺序与畸形项处置在框架入口统一。复用 `core/locale` 匹配和目录回退，应用只装配支持语言和资源。[HTTP Accept-Language](https://www.rfc-editor.org/rfc/rfc9110.html#name-accept-language) · [gRPC Metadata](https://grpc.io/docs/guides/metadata/) · [Go 语言解析](https://pkg.go.dev/golang.org/x/text/language#ParseAcceptLanguage)

## 通过条件

- 独立模块覆盖多个字段、权重、并列顺序、畸形项、零权重、通配符和上下文快照。
- 从真实 Assistant 进程通过 Kronos HTTP、gRPC 和 MicroRPC 客户端验证简体中文及默认英文文案，并比较稳定业务错误码。SSE 验证同一语言规则和首帧前的 HTTP 失败。
- 通过生成的 Order 客户端和真实 etcd 验证标准 gRPC 与 MicroRPC 的缺省、多个字段、权重并列及无效项。验证上游函数收到的入站偏好不会自动转发，传输收到的偏好也不进入服务端的出站上下文。
- 保留模块竞态、跨层和 Protobuf 生成检查。使用 Linux CI 验证相关模块与真实子进程。

## 实测与复盘

- Go 1.25.0、macOS arm64 的首次验收通过全仓测试、分层与生成检查。`GOWORK=off go vet ./...`、独立模块的 `go vet ./...` 与 `go mod verify` 通过。子进程测试核对 HTTP 与 gRPC 一元、gRPC 流及 SSE 的本地化错误。[进程测试](../../examples/assistant/cmd/assistant/main_test.go) · [当前 Go 验收入口](go-verification.md)
- `framework/localization` 复用 `core/locale` 已采用的 `golang.org/x/text v0.40.0`，参考应用依赖也对齐该版本。Ubuntu 的 `Go checks` 在 `a2ac28d` 通过全仓测试，覆盖语言协商独立模块与 Assistant 本地化子进程测试。协议生成和分层检查同时通过。[Linux CI](https://github.com/fangfengxiang/kronos/actions/runs/45097486370)
- Go 1.25.0、Ubuntu 24.04 Linux amd64 和真实 etcd v3.6.16 运行带 `etcd_interop` 标签的 `TestOrderPropagatesLanguageAcrossBothProtocols` 八轮。gRPC 与 MicroRPC 均保持多个字段的权重和并列顺序，默认回退为英文，上游入站偏好没有进入下游请求。完整的 `-tags=interop,etcd_interop` 互通、观测和恢复套件通过。Linux arm64 的仓库内脚手架与独立 Go 工作区也运行了两种协议。[双协议语言联调](../../test/interop/microv2/localization_test.go)
- Ubuntu 24.04 Linux arm64 以 `-count=2 -run '^TestAssistantProcessShutsDownOnSIGTERM$'` 执行真实 Assistant 进程测试。四入口的语言回退、MicroRPC 的简体中文与英文错误以及稳定错误码、真实指标和 Trace 断言均通过。macOS arm64 上的 Assistant 竞态回归与 Linux Compose 端到端通过。[Assistant 进程测试](../../examples/assistant/cmd/assistant/main_test.go)
