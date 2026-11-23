# Blades 文本 Agent 适配

## 消费者与最小契约

首个消费者是 Assistant 的 `Run` 与 `StreamRun`。应用使用确定性文本模型，在一元入口汇总结果，在 gRPC 流与 SSE 入口按顺序发送增量。模型语言随输入和模型实现确定，提示词校验继续使用应用的本地化资源。

`infra/blades` 是独立 Go 模块。应用传入 Agent 名与 `TextModel`，后者接收标准上下文和单条提示词，按顺序产生字符串与错误。`Runner.Run` 汇总同一流执行路径的输出。`Runner.Stream` 同步交付增量，直到观察到成功模型回合和运行终态。模型需要响应上下文取消，并遵守迭代器的停止信号。

## 运行与失败

- 将提示词转换为 Blades 的 `event.Prompt`，按调用分别运行。只公开文本增量。成功终态由 Assistant 的协议绑定发送。
- 模型失败和调用方取消沿调用链返回。下游接收失败时取消执行，继续读完 Blades 事件以释放其输出循环。缺少成功回合或运行终态返回 `ErrIncomplete`。一元失败时返回空结果，流式失败时保留已交付的片段。
- 传输仍以 Assistant 的稳定错误标识和失败状态回应。模型驱动只在应用装配点选择，本切面装配确定性替身。

## 依赖与取舍

参考服务在调用边界聚合模型增量并回收流。保留顺序、原始提示词和资源收尾方式。复用 Blades v0.6.0 的 `NewAgent`、`Runner.RunStream`、`model.Collect` 和事件定义。该版本声明 Go 1.25.0。适配层持有 Blades 类型并处理成功、失败与取消。Blades v0.6.0 的终态发送会等待输出 channel 可写，事件消费者在出错后继续读取直至关闭。[Blades v0.6.0](https://github.com/go-kratos/blades/releases/tag/v0.6.0) · [上游 API](https://pkg.go.dev/github.com/go-kratos/blades@v0.6.0)

## 通过条件

- 独立模块测试验证 Unicode 和空白保留、增量顺序、并发调用隔离、模型提前或中途失败、客户端取消和接收方拒绝后模型退出。
- 真实 Assistant 进程用同一模型验证一元结果、gRPC 增量与完成、SSE 忽略其他内部事件，以及既有语言协商与关停行为。
- Linux 构建并实际运行组件和参考应用，记录命令及证据。应用协议产物通过生成一致性检查。

## 实测与复盘

- 在 Ubuntu 24.04 的 Linux arm64 容器使用 Go 1.25.0，关闭工作区后执行 `go build -o /tmp/kronos-assistant ./examples/assistant/cmd/assistant`、`go -C infra/blades test -count=10 ./...` 和 `go test -count=1 ./examples/assistant/...`，均通过。Assistant 测试覆盖真实子进程和回环连接。[子进程测试](../../examples/assistant/cmd/assistant/main_test.go)
- macOS arm64 下通过 `bash scripts/test-modules.sh`、`bash scripts/check-layer-deps.sh`、`bash tools/proto/generate.sh --check`、根模块和适配模块的 `GOWORK=off go vet ./...`，以及适配模块的 `go mod verify`。SSE 集成测试通过真实 Blades Runner 验证首帧前失败、流中异常、公开错误标识和模型收到断开取消。[SSE 测试](../../examples/assistant/sse_test.go)
- 使用 `GOWORK=off GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C infra/blades test -c` 生成 Windows 测试二进制。此项记录为交叉构建。
