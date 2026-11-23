# 请求指标

## 消费者与契约

Assistant 的运维方可以按业务方法检查请求数量和处理耗时。通过 `-metrics` 或启动 JSON 的 `"metrics":"true"` 开启指标，在主 HTTP 监听的 `/metrics` 抓取。每个应用实例持有独立指标注册表。

| 指标 | 内容 |
| --- | --- |
| `kronos_rpc_requests_total` | 进入业务入口并退出的请求计数。 |
| `kronos_rpc_request_duration_seconds` | 同一请求的入口至返回耗时，单位为秒，使用 Prometheus 直方图。 |

指标标签为启动时登记的 `operation` 与受控的 `outcome`。Assistant 登记 `Run` 和 `StreamRun`，`outcome` 使用 `success` 与 `error`。HTTP、gRPC、SSE 和 go-micro v2 的同一业务方法计入同一组指标。流式调用在整个方法返回时记录一次，接流拒绝、校验失败、取消、模型失败和已恢复的业务 panic 归入 `error`。输入提示词、用户字段、错误文案和请求 ID 不参与标签。[请求恢复](rpc-handler-recovery.md)

## 边界与取舍

- 参考服务在业务操作附近记录固定功能名、执行结果和耗时。参考 Prometheus 类库复用原生计数器封装。Kronos 沿用固定操作名和完成时的记录点，以独立实例隔离注册，避免共享进程写入相同指标。
- `infra/prometheus` 隐藏 `client_golang` 的注册表、计数器和直方图。调用者传入业务方法、结果和标准 `time.Duration`，以标准 `http.Handler` 挂载抓取入口。未知方法和负时长被忽略，空操作列表、空名称或重复注册返回错误。计数和直方图的聚合、抓取格式由 Prometheus 官方库负责。[Prometheus Go 指南](https://prometheus.io/docs/guides/go-application/) · [指标实践](https://prometheus.io/docs/practices/instrumentation/)
- Kratos 的 HTTP 与 gRPC 接线继续提供实际监听。指标在公共业务入口收集，复用现有请求计时，并覆盖兼容协议和 SSE 的整次调用。当前采用 `client_golang v1.23.2` 的注册表和抓取处理器。OTel 链路追踪按独立切面验收。[Prometheus 固定版本](https://github.com/prometheus/client_golang/releases/tag/v1.23.2) · [Kratos OTel 扩展](https://github.com/go-kratos/kratos/tree/v3.0.0/contrib/otel)

## 通过条件

- Go 测试对照直接使用 Prometheus 注册表和 Kronos 包装的计数、直方图、标签及秒单位。核对两实例隔离、未知方法和负时间处理。
- Linux 上运行实际 Assistant，三个一元入口和两种流入口可调用。抓取结果能按方法和结果归计，默认启动的抓取路由返回 404。
- Go `Example` 展示实例装配和直接记录。用容器运行相同采集入口并记录实测平台。

## 实测与复盘

- Go 1.25.0、Ubuntu 24.04 Linux arm64 上通过 `GOWORK=off go -C infra/prometheus test -count=10 ./...` 和 `GOWORK=off go test -count=1 ./examples/assistant/...`。真实进程抓取到 `Run` 4 次成功、7 次失败，`StreamRun` 成功和失败各 2 次，并检查直方图样本、默认 404 和标签范围。[进程指标测试](../../examples/assistant/cmd/assistant/metrics_test.go)
- 向官方 Prometheus 计数器和直方图直接写入一条 1.5 秒观测，再经 Kronos 写入 `1500*time.Millisecond`。两种路径抓取得到相同的标签、计数、样本数与 `1.5` 秒和。竞态测试对同一实例并发写入，另一个实例保持零计数。复核在登记时创建指标系列，抓取能观察到当前实例未出现请求时的零值。[对照与实例测试](../../infra/prometheus/requests_test.go) · [可执行示例](../../infra/prometheus/example_test.go)
- macOS arm64 上全模块竞态测试、分层检查和 `go vet` 通过。Docker Compose 的 Linux arm64 进程通过 `/metrics`、四入口和关停测试。`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C infra/prometheus test -c` 交叉编译通过，Windows 证据为构建结果。[容器测试](../../test/e2e/assistant/compose_test.go)
