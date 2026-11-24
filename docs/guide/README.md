# 使用指南

Kronos 的 Core 原语已可通过 Go 包使用。Assistant v1 提供一元 HTTP 和 gRPC，并可独立启用 SSE 和 go-micro v2 兼容监听。

在仓库内通过代理接线时，可使用 `kronos-framework` Skill 查阅生成、装配与验收步骤。[框架使用 Skill](../../.agents/skills/kronos-framework/SKILL.md)

| 我想做什么 | 当前可以查阅 |
| --- | --- |
| 判断 Kronos 是否适合我的 Go 服务 | 阅读框架的责任范围与依赖方向。[项目首页](../../README.md) · [架构概览](../architecture/overview.md) |
| 使用 Core 包 | 核对执行边界、语言选择与消息回退，运行包级单元测试和示例。[Core 首版落地设计](../design/core-v1.md) |
| 启动并等待后台任务 | 用 `core/runtime.Go` 运行批处理，使用观察回调接收终态。[协程契约](../design/runtime-go.md) · [后台任务示例](../../examples/runtimejob) |
| 用 Proto 声明服务并提供 gRPC 和 HTTP | 确认服务定义、HTTP 映射和生成物的权威来源。[协议与传输决定](../adr/0001-protobuf-and-transport.md) |
| 发布一个可取消的 Agent 流 | 了解公开流事件、客户端取消和确定性模型装配。[Agent 流式决定](../adr/0002-agent-stream.md) · [Blades 文本 Agent 适配](../design/blades-adapter.md) |
| 为同一个服务提供中英文体验 | 分辨请求语言偏好、系统消息和模型生成内容，查看默认回退。[本地化决定](../adr/0003-localization.md) |
| 按用户时区展示时刻或归档日报 | 用 `core/timezone` 解析 IANA 时区并计算当地日期。精简部署可通过 `-tags timetzdata` 内嵌规则。[Core 时区](../design/core-timezone.md) · [日报日期示例](../../examples/reportdate) |
| 在本机复现端到端测试 | 用 Compose 和 Go 测试验证镜像、探针、真实调用与容器关停。[容器交付](../design/assistant-compose.md) |
| 使用 etcd 分协议注册或发现地址 | `infra/etcd` 提供独立客户端与租约发布。Windows 开发可连接 Linux etcd，官方 Windows 服务端为 Tier 3。[适配契约与实测](../design/service-governance.md#etcd-适配契约) |
| 用统一入口部署 gRPC 和旧 RPC | 从 Order 的 22 行装配开始，复用同一套业务实现。[Order 服务](../../examples/order/cmd/order/main.go) · [统一服务端](../design/unary-service-api.md) |
| 按服务名调用 Order | 生成客户端接收一个 Kronos 客户端。默认 gRPC，显式参数选择兼容协议。[Order 客户端命令](../../examples/order/cmd/order-client/main.go) · [发现与连接生命周期](../design/discovered-rpc-client.md) |

## 验证独立模块

从仓库根目录调用与 CI 相同的入口。

```sh
go run ./platform/verify deps
go run ./platform/verify test
```

入口读取 `go.work` 内登记的模块，在各自目录关闭工作区执行检查和原生 Go 测试。新增封装模块时用 `go work use ./模块目录` 登记。[Go 验收入口](../design/go-verification.md) · [模块边界](../adr/0004-layer-and-module-boundaries.md)

## 运行 Order

准备可访问的 etcd v3。以下命令启动与 Linux 验收相同的官方镜像，宿主端口仅向本机开放。命令在终端运行，结束后容器自动清理。

```sh
docker run --rm -p 127.0.0.1:2379:2379 \
  -e ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379 \
  -e ETCD_ADVERTISE_CLIENT_URLS=http://127.0.0.1:2379 \
  -e ETCD_DATA_DIR=/tmp/kronos-etcd \
  gcr.io/etcd-development/etcd@sha256:b29d816b546a1966fe7034a33b34e231fa98c47ce4d2883f70b0555020b22132
```

Order 默认连接 `127.0.0.1:2379`，启动 gRPC `127.0.0.1:9090` 和 MicroRPC `127.0.0.1:9091`。部署可设置 `KRONOS_ETCD_ENDPOINT`、`KRONOS_ORDER_GRPC`、`KRONOS_ORDER_MICRORPC` 和 `KRONOS_ORDER_ADVERTISE_HOST`。服务名默认为 Proto 全名，旧系统迁移时用 `KRONOS_ORDER_NAME` 设置双方约定的发现名。[应用装配](../../examples/order/cmd/order/main.go)

```sh
GOWORK=off go run ./examples/order/cmd/order
```

在另一个终端执行以下原生 Go 调用。需要连接其他发现名的兼容部署时，加上 `-service <服务名>`。

```sh
GOWORK=off go run ./examples/order/cmd/order-client -id 订单-01
GOWORK=off go run ./examples/order/cmd/order-client -protocol micro-v2 -id 订单-01
```

两个命令均返回 `订单-01 confirmed`。使用自定义服务名时在客户端带上 `-service`。修改 Order 的 Proto 后执行 `go run ./platform/verify generate` 更新公开契约。[Order Proto](../../examples/order/api/kronos/order/v1/order.proto) · [治理验收](../design/service-governance.md)

在 Order 进程设置 `KRONOS_ORDER_DIAGNOSTICS=127.0.0.1:9190` 可启用 `/livez`、`/readyz` 和 Prometheus `/metrics`。诊断端口单独绑定。观察可发现状态时按对应协议查询 etcd，再执行 Order 调用确认业务结果。[Order 运行观测](../design/order-diagnostics.md)

设置 `KRONOS_ORDER_TRACE_ENDPOINT=http://127.0.0.1:4318/v1/traces` 将 Trace 发到本机 OTLP HTTP 接收端。应用开启 JSON 日志，在失败日志中按 `trace_id` 和 `span_id` 查找对应 span。Go 调用方可把自己创建的 `oteltrace.Provider` 通过 `kronos.Tracing` 交给客户端，并在关闭客户端后释放 provider。[跨协议 Trace](../design/order-diagnostics.md#trace-与调用定位)

以下示例单独启动 Order 与两版官方 go-micro v2。旧客户端发现并调用 Order，Order 客户端命令显式用 `micro-v2` 查找旧服务端，返回 `订单-03 legacy-confirmed`。

```sh
KRONOS_ETCD_TEST_ADDRESS=127.0.0.1:2379 GOWORK=off go test \
  -tags=interop,etcd_interop -count=1 -run '^TestOrderThroughOfficialRegistryAndClients$' -v ./test/interop/microv2
```

完整验收覆盖断连、进程崩溃、租约恢复和双协议摘流。[CI 与本地命令](../design/go-verification.md#order-治理验收) · [RPC 日志字段](../design/structured-logging.md#统一一元-rpc-的事件)

## 生成 Assistant 协议

准备 Go 1.25.0 或更高版本，并将 [Buf 1.70.0](https://github.com/bufbuild/buf/releases/tag/v1.70.0) 放入 `PATH`。首次生成需要访问公开的 Go 模块代理和 Buf Schema Registry。生成命令从仓库根目录执行。

```sh
go run ./platform/verify generate
go run ./platform/verify proto
go test ./examples/assistant/...
```

修改 [Assistant Proto](../../examples/assistant/api/kronos/assistant/v1/assistant.proto) 后重新生成，提交协议、`examples/assistant/internal/wire` 内的私有绑定及公开契约 `contract.kronos.pb.go`。检查命令校验格式、lint 和重新生成的内容，CI 执行同一入口。[Assistant v1 协议设计](../design/assistant-protocol.md) · [公开契约生成](../design/protobuf-go-contract.md)

Assistant 的业务实现提供 `Service.Run`。调用方通过 `Run` 发起请求，由实例所有者执行 `Close`。HTTP 客户端使用 `NewHTTPClient`。RPC 客户端使用 `NewRPCClient`，默认走标准 gRPC。通过 `rpc.WithProtocol` 可选用 go-micro v2。[集成用例](../../examples/assistant/assistant_test.go) · [RPC 客户端协议](../design/rpc-client-protocol.md)

```go
standardClient, err := assistant.NewRPCClient(ctx, "127.0.0.1:9090", time.Second)
legacyClient, err := assistant.NewRPCClient(ctx, "127.0.0.1:9091", time.Second,
    rpc.WithProtocol(rpc.ProtocolMicroV2))
```

标准 gRPC 客户端通过 `Client.StreamRun` 调用服务端流。回调读取 `GetTextDelta()`，在成功结束后收到 `GetCompleted()`。回调返回错误会结束调用并取消服务端工作。[可执行的 Go 客户端示例](../../examples/assistant/stream_example_test.go) · [流终态与失败规则](../design/rpc-stream-client.md)

### 关联请求

服务方法通过 `request.ID(ctx)` 读取请求标识。将同一个上下文传给 Kronos 的 HTTP 或 gRPC 客户端可继续传播该标识，调用方可用 `request.WithID` 设置自己的标识。go-micro v2 服务方法会收到本次调用的本地标识。[可执行的 Go 示例](../../framework/request/example_test.go) · [请求元数据契约](../design/request-metadata.md)

参考服务的绑定将业务 panic 转换为现有的服务错误，并记录请求标识、类型和栈。自定义绑定可用 `rpc.RecoverUnary` 和 `rpc.RecoverStream` 包装业务处理函数，再沿本服务的错误映射返回。[恢复设计与实测](../design/rpc-handler-recovery.md) · [可执行的 Go 示例](../../framework/rpc/recovery_test.go)

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

进程在标准错误流记录逐行 JSON。可按 `event` 查询 `listener_starting`、`request_finished`、`reload_failed` 和 `app_stopped`，并用 `request_id` 关联业务调用。其他 Go 服务可通过 `log.NewJSON` 构造独立实例。[结构化日志](../design/structured-logging.md) · [可执行的 Go 示例](../../framework/log/example_test.go)

使用 `-metrics` 或在启动 JSON 设置 `"metrics":"true"`，即可在主 HTTP 监听的 `/metrics` 抓取请求量和延迟。Compose 示例配置已启用抓取。[指标使用设计](../design/request-metrics.md) · [可执行的 Go 示例](../../infra/prometheus/example_test.go)

使用 `-trace-endpoint http://127.0.0.1:4318/v1/traces` 或同名 JSON 配置对接 OTLP HTTP Collector。配置地址从 Assistant 进程所在环境访问。请求处理日志附带 `trace_id` 和 `span_id`，正常退出前进程清理 exporter。[链路追踪设计](../design/request-tracing.md) · [可执行的 Go 示例](../../infra/oteltrace/example_test.go)

使用 `-runtime-profile 127.0.0.1:6060` 或在启动 JSON 设置 `"runtime-profile":"127.0.0.1:6060"`，即可从 Assistant 所在的网络空间读取 heap 或 goroutine 快照。[按需运行时快照](../design/runtime-profiles.md) · [可执行的 Go 示例](../../infra/runtimeprofile/example_test.go)

```sh
go tool pprof -top http://127.0.0.1:6060/debug/pprof/heap
go tool pprof -top http://127.0.0.1:6060/debug/pprof/goroutine
```

### 连接独立文本后端

分别启动文本后端和 Assistant。第二条命令为 Assistant 的当前进程接入后端，也可以在启动 JSON 设置 `"text-backend":"127.0.0.1:9290"`。[调用设计](../design/text-backend-rpc.md) · [可执行的 Go 示例](../../examples/textbackend/example_test.go)

```sh
go run ./examples/textbackend/cmd/textbackend -grpc 127.0.0.1:9290
```

```sh
go run ./examples/assistant/cmd/assistant -text-backend 127.0.0.1:9290
```

TextBackend 可以向已有 Consul agent 注册 `textbackend` 实例。部署在独立网络或使用通配监听时，通过 `-advertise-grpc` 公布 agent 和调用方都能访问的 `host:port`。调用侧可以用 `infra/consul.Registry.Lookup` 获取当前地址，或用 `Watch` 订阅变化。[注册与订阅设计](../design/consul-service-registration.md) · [可执行的 Go 示例](../../infra/consul/example_test.go)

```sh
go run ./examples/textbackend/cmd/textbackend -grpc 127.0.0.1:9290 -consul http://127.0.0.1:8500
```

Assistant 用同一个 Consul agent 按名称发现已注册并通过健康检查的实例。使用 `-text-backend-consul` 时省略 `-text-backend`。两项皆为空时使用进程内文本源。[动态调用设计](../design/text-backend-discovery.md) · [可执行的 Go 示例](../../examples/textbackend/example_test.go)

```sh
go run ./examples/assistant/cmd/assistant -text-backend-consul http://127.0.0.1:8500
```

TextBackend 可从 MySQL 读取文本前缀。先在部署库应用表定义并写入具名配置，通过环境变量 `KRONOS_TEXTBACKEND_MYSQL_DSN` 设置连接信息。启用后，后端按名称读取当前值。[SQL 表定义](../../examples/textbackend/schema/mysql.sql) · [MySQL 用法与实测](../design/mysql-text-profiles.md)

```sh
go run ./examples/textbackend/cmd/textbackend -mysql-profile greeting
```

若为 MySQL 配置启用 Redis 缓存，设置 `KRONOS_TEXTBACKEND_REDIS_URL` 和 `-profile-cache-ttl`。共享 Redis 的部署分别给出 `-profile-cache-namespace`。Cluster 配置使用 `-profile-cache-cluster`，可在 URL 中用多个 `addr` 增加种子地址。[Redis 缓存用法与实测](../design/redis-text-profile-cache.md) · [可执行的 Go 示例](../../infra/redis/integration_test.go)

```sh
go run ./examples/textbackend/cmd/textbackend -mysql-profile greeting -profile-cache-ttl 5s -profile-cache-namespace demo
```

用 MongoDB 存储文本配置时，在目标数据库的 `text_backend_profiles` 集合写入 `{_id: "greeting", output_prefix: "你好："}`，再设置 `KRONOS_TEXTBACKEND_MONGODB_URI` 并选择配置 ID。`-mongo-database` 默认为 `kronos`。同一进程选择一个文本配置来源。[MongoDB 用法与实测](../design/mongodb-text-profiles.md) · [可执行的 Go 示例](../../infra/mongodb/integration_test.go)

```sh
go run ./examples/textbackend/cmd/textbackend -mongo-profile greeting -mongo-database kronos
```

用 OSS 存储文本配置时，将 UTF-8 前缀写入对象，再设置 `KRONOS_TEXTBACKEND_OSS_REGION`、`KRONOS_TEXTBACKEND_OSS_BUCKET`、`OSS_ACCESS_KEY_ID` 和 `OSS_ACCESS_KEY_SECRET`。短期凭据同时设置 `OSS_SESSION_TOKEN`。默认连接区域 HTTPS endpoint。使用桶专属域名时设置完整的 `KRONOS_TEXTBACKEND_OSS_ENDPOINT` 并传入 `-oss-cname`。入口在启动时读取凭据，轮转后重启进程。[OSS 用法与实测](../design/oss-text-profiles.md) · [可执行的 Go 签名示例](../../infra/aliyunoss/bucket_test.go) · [OSS endpoint 与凭据](https://www.alibabacloud.com/help/en/oss/developer-reference/manual-for-go-sdk-v2/)

```sh
go run ./examples/textbackend/cmd/textbackend -oss-profile profiles/greeting.txt
```

### 记录生成事件

先创建 Kafka 主题，再分别启动 TextBackend 和事件消费命令。TextBackend 只有在 broker 确认后才返回成功。消费命令按行输出 JSON，写入成功后推进所选消费组的位点。组首次运行时读取最早可用记录。[Kafka 消息设计](../design/kafka-generation-events.md) · [事件 Protobuf](../../examples/textbackend/api/kronos/textbackend/v1/text_backend.proto)

```sh
go run ./examples/textbackend/cmd/textbackend -kafka-brokers 127.0.0.1:9092 -kafka-topic text-generation
```

```sh
go run ./examples/textbackend/cmd/textbackend-events -kafka-brokers 127.0.0.1:9092 -kafka-topic text-generation -kafka-group analytics
```

两个命令均可用 `-kafka-tls` 启用 TLS，或用 `-kafka-ca-file` 指定 PEM 根证书。SASL PLAIN 凭据来自 `KRONOS_KAFKA_SASL_USERNAME` 和 `KRONOS_KAFKA_SASL_PASSWORD`，使用凭据部署时启用加密监听。[Apache Kafka 安全模型](https://kafka.apache.org/44/security/security-model/)

### 通过 Docker Compose 运行

从仓库根目录构建并等待容器就绪。HTTP、gRPC、SSE 和 go-micro v2 分别使用容器端口 `8080`、`9090`、`8081`、`9091`，按下方命令查询宿主回环地址。挂载配置定义各监听和应用关停预算。[Compose 配置](../../compose.yaml) · [容器启动配置](../../examples/assistant/deploy/assistant.json)

```sh
docker compose up --build --wait
docker compose port assistant 8080
docker compose logs assistant
docker compose down
```

Go 端到端测试使用隔离的 Compose 项目，运行四个实际入口并验收启动错误和 `SIGTERM`。需要可用的 Linux 容器引擎。[Go 调用示例](../../test/e2e/assistant/compose_test.go) · [容器实测](../design/assistant-compose.md)

```sh
go test -tags=e2e -count=1 ./test/e2e/assistant
```

### 同时启用 SSE 与旧协议

```sh
go run ./examples/assistant/cmd/assistant \
  -http 127.0.0.1:8080 -grpc 127.0.0.1:9090 \
  -sse 127.0.0.1:8081 -micro-v2 127.0.0.1:9091
```

```sh
curl --no-buffer -fsS http://127.0.0.1:8081/v1/assistant:streamRun \
  -H 'Content-Type: application/json' -H 'Accept: text/event-stream' -d '{"prompt":"你好"}'
```

SSE 的请求体使用 JSON。调用示例的 go-micro v2 服务名是 `kronos.assistant.v1.Assistant`，端点名是 `Assistant.Run`。Go 互通测试自行启动服务，按版本构建官方客户端和服务端。首次运行需要访问 Go 模块代理或使用模块缓存。[原生 Go 互通验收](../design/go-micro-v2-interop.md) · [可执行协议选项示例](../../framework/rpc/example_test.go)

旧协议的有帧响应上限为 1 MiB，包含两个长度字段、信封和业务消息。超限时服务端使用旧 RPC 错误信封报告 `RESPONSE_TOO_LARGE`，Assistant 的 Kronos 客户端继续返回 `ERROR_REASON_RUN_FAILED`。[兼容范围](../design/additional-listeners.md#go-micro-v2-兼容范围)

```sh
go test -tags=interop -count=1 ./test/interop/microv2
```

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

显式参数覆盖文件，文件缺失的键沿用默认值。文件不存在、出现未知键或生效值无效时，入口返回启动错误。[可用配置项和检查规则](../design/config-file.md) · [可执行的 Go 示例](../../infra/configfile/example_test.go)

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
