# Kafka 消息与文本生成事件

TextBackend 可向配置的 Kafka 主题写入 `kronos.textbackend.v1.GenerationRecorded`。其分区键和 `request_id` 来自当前 RPC 的请求标识，`output_utf8_bytes` 记录返回文本的 UTF-8 字节数。事件消费命令 `textbackend-events` 将记录输出为 JSON，每行包含请求标识、字节数与 Kafka 位点。消息中无提示词或生成文本。

## 契约与所有权

- `infra/kafka` 持有 franz-go 客户端。`Config` 明确指定 broker 种子与一个主题，使用标准 `tls.Config` 可选启用 TLS，使用 `PlainAuth` 可选启用 SASL PLAIN。部署者预先创建主题。公开消息用字节切片、分区及位点表达，由业务进行 Protobuf 编解码。
- `OpenPublisher` 探活 broker，失败时关闭已建立的客户端。`Publish` 等待 Kafka 对所有同步副本确认，保留 SDK 的幂等生产。若调用期限先结束，发布可能已到达 broker，重试可能产生第二条业务记录。应用停止接流并排空调用后关闭发布者。
- `NewConsumer` 绑定一个消费组。`Run` 持有本轮客户端，从提交位点开始，没有位点时读最早可用记录。每条记录按顺序处理并同步提交下一位点，处理失败或提交失败时结束本轮。`Run` 收尾释放再平衡限制、离开组及关闭连接。处理函数使用调用方上下文，重复投递由消费者按业务语义处理。[Kafka 投递语义](https://kafka.apache.org/22/design/design/#semantics) · [franz-go 偏移管理](https://github.com/twmb/franz-go/blob/v1.21.7/docs/producing-and-consuming.md#offset-management)
- 应用装配启用 Kafka 后，TextBackend 在本次 RPC 的期限内等待事件写入，内部发布预算为 500 毫秒。内部发布超时或 broker 失败通过现有 `ERROR_REASON_GENERATION_FAILED` 报告，调用方取消继续使用 RPC 的取消语义。消费命令成功写出 JSON 后才允许提交位点。CLI 从环境读取 SASL 凭据，TLS 使用系统根或指定 CA 并校验服务名。

## 复用与取舍

参考业务先序列化具体事件再以 broker 确认进行同步发送，消费成功后才推进位点。当前保留这些可观察的交付语义，应用持有自己的 Protobuf 消息，适配只收发字节。franz-go v1.21.7 在 Go 1.25.0 上提供幂等生产、TLS/SASL、可取消轮询和显式再平衡控制。Sarama 延续原有同步发送模型，当前版本要求 Go 1.26。`kafka-go` 支持确认与手动提交，也适合该切面。franz-go 提供本切面需要的显式分区所有权控制和纯 Go 发布路径。[franz-go v1.21.7](https://github.com/twmb/franz-go/releases/tag/v1.21.7) · [当前 Sarama Go 约束](https://github.com/IBM/sarama/blob/v1.62.0/go.mod) · [kafka-go Reader/Writer](https://github.com/segmentio/kafka-go/tree/v0.4.51)

每次处理后提交一次位点，便于验证边界。吞吐量有批处理需求时，可依据真实消费者再决定批量策略。运行中保留对进程崩溃后重投的预期，具体取舍见 [Kafka 确认与位点决定](../adr/0008-kafka-delivery-and-offsets.md)。

## 通过条件

- Ubuntu 上用 Apache Kafka 与 franz-go 原生客户端对照，验证 Protobuf 字节与分区键、broker 确认、发布取消、真实位点提交与失败记录重读。验证 TLS 与 SASL PLAIN、错误 CA 或凭据拒绝以及关闭行为，提供原生 Go `Example`。
- 启动真实 TextBackend 与事件消费命令，从公开 gRPC 入口发送带请求标识的 Unicode 文本，检查消费者 JSON 与 RPC 响应的字节数一致。消费命令与应用收到 `SIGTERM` 后退出。发布期限失败保持稳定 RPC 错误。
- 验证 Buf 格式、生成结果、独立模块依赖、跨层导入及 Windows 构建，记录实际 Linux broker 和测试条件。

## 实测记录

- Go 1.25.0、Ubuntu 24.04 Linux arm64，连接隔离网络中运行的官方 Apache Kafka 4.4.0 单节点 KRaft。Kafka 发行包通过官方 SHA-512 文件核验，摘要为 `f5ceba8a265ab6bb2d68362b1f6859a95e15d60ef92ba83b43fdf065b557dc18e98d6383c15b9977320e25b3a2c8b6f11decb17b0fa6c0f163423793a0eb95cb`。broker 使用 Linux arm64 Temurin 21 JRE，镜像摘要为 `sha256:dff6eb46527bd293a3c06bbbe9e64e8c9f91f01905b5abbdad31761c03059d2c`。[Apache Kafka 发行包](https://kafka.apache.org/community/downloads/)
- 在该环境执行 `GOWORK=off go -C infra/kafka test -tags=kafka_integration -count=3 -v ./...`，三轮通过。原生客户端交叉读写、取消启动上下文后继续发布、错误处理后的组位点重读、短期限发布、关闭后拒绝、TLS/SASL PLAIN 以及错误 CA 和凭据拒绝均实际运行。[原生 Go 对照](../../infra/kafka/integration_test.go)
- 同环境执行 `GOWORK=off go test -tags=kafka_integration -count=3 -v ./test/integration/kafka`，三轮通过。真实 TextBackend 与消费命令走 TLS/SASL，Unicode 输出字节数与事件一致，未取得写入确认时返回稳定 RPC 失败，子进程收到 `SIGTERM` 后正常退出。[进程集成测试](../../test/integration/kafka/generation_test.go)
- `KRONOS_KAFKA_TEST_BROKERS` 指向允许创建测试主题的隔离 broker。进程验收传入预建的单分区 `KRONOS_KAFKA_TEST_TOPIC`，测试 broker 关闭自动创建主题。加密验收同时提供 `KRONOS_KAFKA_TEST_TLS_BROKERS`、`KRONOS_KAFKA_TEST_CA_FILE`、`KRONOS_KAFKA_TEST_SASL_USERNAME` 和 `KRONOS_KAFKA_TEST_SASL_PASSWORD`。
- 启动故障对照中，原始 `Ping` 在 broker 接受 TCP 后停在 Kafka 协议握手，消耗了 Go 测试的 3 秒验收期限。接通 SDK 客户端上下文后，macOS `GOWORK=off go -C infra/kafka test -race -count=5 -v ./...` 五轮通过，两条取消路径均释放连接。真实 Kafka 测试验证启动完成后发布者仍由调用端关闭。
- Buf 与两份服务的产物一致性检查、`go run ./platform/verify deps`、`go vet -tags=kafka_integration ./...` 和全模块竞态测试通过。Assistant 默认 Linux Compose 回归通过。`CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build` 验证 Kafka 模块和两个参考命令可构建，Windows 尚无进程运行记录。
