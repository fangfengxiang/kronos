# Protobuf 的 Go 公开契约生成

## 消费者与契约

服务使用者需要从应用公开包获得 Protobuf 请求、响应、枚举与服务方法类型。`platform/protogen` 提供独立的 Go 生成插件。一次调用接收同一服务的 Proto3 文件，通过 `api_import` 指定应用公开 Go 包。各文件的 `go_package` 指向该应用的 `internal/wire` 子目录。

- 生成 `contract.kronos.pb.go`，公开消息、枚举、枚举值和 Open Go API 的 `oneof` 分支类型。生成 `<Service>UnaryService` 接口，其方法签名使用 `context.Context` 和协议声明的请求响应。已生成的协议类型以 Go 别名公开，保持描述符、序列化和类型身份。
- 已发布的 Assistant `Service` 和短错误枚举名称继续由公开包提供。实际应用实现采用生成的一元接口。流事件使用生成的公开类型，流业务回调、不同传输的连接及注册由服务现有装配提供。[RPC 流客户端](rpc-stream-client.md)
- 插件拒绝公开包无效、输入私有路径不属于该服务、公开名称冲突和未公开的私有方法签名类型。错误包含被拒绝的输入名称。支持 Go Protobuf Open API 和 Proto3 optional。

## 边界与取舍

- 参考服务从同一 Proto 文件生成消息和不同的传输代码，业务调用透过服务接口使用消息。沿用一份协议源和固定的生成入口。直接复用 `protogen` 的请求处理、Go 名称和输出格式，Kronos 只生成公开契约。Go-Protobuf、gRPC、Kratos HTTP 插件继续生成私有产物。[Go Protobuf 生成规则](https://protobuf.dev/reference/go/go-generated/) · [Go protogen](https://pkg.go.dev/google.golang.org/protobuf@v1.36.11/compiler/protogen) · [Buf 生成](https://buf.build/docs/generate/)
- 同一份消息定义由官方生成器拥有。公开契约采用 Go 类型别名，避免两套 Protobuf 定义及转换。传输库的具体类型留在服务私有目录。验证工具检查公开产物漂移并限制业务直接导入传输库。[原生 Go 验收](go-verification.md)

## 通过条件

- Go 包测试覆盖多个输入文件的别名和方法生成、可定位的非法包路径与名称冲突。
- 从实际 Assistant Proto 同时生成官方私有 Go 绑定与公开契约，验证可重复生成。用公开别名和私有生成类型交叉编码与解码，核对字段、流事件和错误标识。
- 在 Linux 用同一生成入口和真实服务运行测试。实际业务实现和 Go 客户端通过公开 API 工作，分层检查继续约束底层导入。

## 实测与复盘

- Go 1.25.0、Buf 1.70.0 在 Ubuntu 24.04 Linux arm64 运行 `go run ./platform/verify proto`、`GOWORK=off go -C platform/protogen test -count=10 ./...` 与 `GOWORK=off go test -count=1 ./examples/assistant/...`，均通过。生成检查同时核对公开契约和私有绑定原文件。[生成模板](../../tools/proto/buf.gen.yaml) · [实际公开产物](../../examples/assistant/contract.kronos.pb.go)
- 对照测试通过同一 Proto 的公开别名和标准私有生成类型交叉序列化，核对流 `oneof`、中文文本、完成事件、描述符与稳定错误枚举。用两个文件覆盖合并生成，并检查跨所有者、同名声明和未公开私有签名的失败条件。[协议对照](../../examples/assistant/contract_test.go) · [插件测试](../../platform/protogen/contract_test.go)
- macOS arm64 上 `go run ./platform/verify generate` 生成公开契约，原有三份私有产物字节一致。`go run ./platform/verify proto`、`deps`、`test` 及独立插件 `go vet` 均通过。复核保留 Assistant 原有的 `Service` 命名类型，通过嵌入生成接口维持公开签名。[公开入口](../../examples/assistant/assistant.go)
