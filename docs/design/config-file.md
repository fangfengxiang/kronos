# 启动配置文件

## 消费者与契约

Assistant 的部署入口需要从挂载文件读取监听地址和关停预算。部署方可传入 `-config <path>`。文件是一个扁平的 JSON 对象，键使用现有启动参数名，值使用相同的字符串写法。`config`、`reload-pid-file` 和 `reload-timeout` 由命令行设置，以便候选进程沿用同一个文件路径、PID 文件和重载控制参数。

| 文件键 | 含义 | 默认值 |
| --- | --- | --- |
| `http` | HTTP 监听地址 | `127.0.0.1:8080` |
| `grpc` | gRPC 监听地址 | `127.0.0.1:9090` |
| `sse` | SSE 监听地址，空字符串关闭 | `""` |
| `micro-v2` | go-micro v2 监听地址，空字符串关闭 | `""` |
| `metrics` | 在 HTTP 监听启用 Prometheus `/metrics`，使用 `"true"` 或 `"false"` | `"false"` |
| `trace-endpoint` | OTLP HTTP 追踪地址，空字符串关闭追踪 | `""` |
| `runtime-profile` | 独立 loopback 快照监听地址，空字符串关闭 | `""` |
| `shutdown-timeout` | 关停时间预算 | `5s` |

优先级从高到低为显式命令行参数、配置文件、命令行参数默认值。显式空字符串或显式默认值也会覆盖文件。文件键和值类型均要通过检查。生效的时长沿用命令行的解析规则。路径按进程的工作目录解析。

## 边界与取舍

- `infra/configfile` 是独立 Go 模块。`LoadJSON[T]` 每次从路径读取一个完整 JSON 对象，解码为调用者指定的类型。结构体解码检查未知字段，映射的键由消费者校验。失败返回零值和包含路径的错误。标准库可识别的读取和解码错误保留原始错误链。
- Assistant 命令入口拥有键、默认值和解析规则。文件读取与校验在组装依赖、获取监听器和发布就绪之前完成。首代启动失败时返回非零退出码。候选进程每次重新读取文件，配置无效时由现有重载流程报告失败，当前进程保留 PID 和监听器。重载控制参数随原命令行传给候选，保障 PID 文件更新到原路径以及候选调用继承流程。[应用装配与关停](app-lifecycle.md) · [应用优雅重载](graceful-reload.md)
- 参考服务在构造传输前一次性将配置填充到类型化结构中。沿用启动时生成配置快照的做法。Kratos 配置组件在加载源后启动 watcher，适用于持续观察配置的消费者。当前消费点只需要每代进程读取一次，使用 Go 标准库读取和解码。参数合并复用 `flag.FlagSet` 的显式参数集合和解析器。模块新增第三方依赖为零。[Kratos Config](https://go-kratos.dev/docs/component/config/) · [Go JSON 解码](https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields) · [Go FlagSet](https://pkg.go.dev/flag#FlagSet.Visit)

## 通过条件

- 模块测试覆盖类型化加载、未知结构体字段、非对象、尾随数据、文件缺失以及解码失败的零值返回。
- 命令入口测试验证文件键和字符串类型检查、实际生效的时长解析，以及显式命令行参数覆盖。
- 在 Linux 运行真实 Assistant。文件提供监听配置，显式参数覆盖 HTTP 地址，四个入口可调用，最终 `SIGTERM` 释放端口。重载遇到无效文件时旧 PID 和四个端口继续服务，恢复正确内容后新 PID 可接流。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 容器内关闭 `go.work`，运行 `go build -o /tmp/kronos-assistant ./examples/assistant/cmd/assistant`、`go -C infra/configfile test -count=10 ./...` 和 `go test -count=1 ./examples/assistant/...`，均通过。子进程用四个真实监听验证配置覆盖与 `SIGTERM`。重载候选因未知配置键退出后，旧 PID 及四个入口继续服务，修正文件后新进程接流。[启动与停止测试](../../examples/assistant/cmd/assistant/main_test.go) · [重载测试](../../examples/assistant/cmd/assistant/reload_test.go)
- 用独立临时模块对照 `LoadJSON[T]` 与 Kratos v3.0.0 的 `config.New`、`file.NewSource`、`Load`、`Scan` 和 `Close`。两者都读出 `{"address":"localhost:8080"}`，也都拒绝对象之后追加第二个 `{}`。对象追加未知字段时，Kronos 返回错误和零值，Kratos 仍读出地址。对照程序记录 Kratos 在成功加载文件时调用一次 `Watch`，Kronos 读取完成即释放文件。Go 1.25.0 的 macOS arm64 与 Ubuntu 24.04 Linux arm64 实际执行结果一致。[Kratos 配置实现](https://github.com/go-kratos/kratos/blob/v3.0.0/config/config.go)
- 在 macOS arm64 上运行全模块竞态测试、跨层依赖检查、协议生成一致性检查、根模块与配置模块的 `go vet` 和 `go mod tidy -diff`，均通过。复核重载控制权后，将 PID 文件和候选启动预算留在原命令行，确保配置文件变更不会让候选绕过监听器继承或改写 PID 路径。
