# Runtime Go

`core/runtime.Go(fn func(), opts ...Option)` 启动一个 goroutine。调用方通过闭包传入业务参数和上下文。各次调用的选项分别生效。

## 恢复与回调

- 在新 goroutine 内通过 `defer` 直接调用 `recover`。捕获 panic 时保留原值和当前堆栈。闭包另起的 goroutine 需要自己的启动边界。
- `WithLogger` 接收 panic 信息。缺少日志回调或日志回调失败时，向标准错误输出 panic 类型和堆栈。默认输出省略原值。
- `WithPanicHandler` 接收相同的 panic 信息，业务失败状态和外部副作用由上层处置。
- `WithObserver` 接收终态及闭包运行时间。终态区分正常返回、panic 与 `runtime.Goexit` 等未正常返回。panic 路径按照日志、处理函数、观察者的顺序调用。
- 回调在同一个 goroutine 中同步执行。回调 panic 单独记录。日志和处理回调即使调用 `runtime.Goexit`，剩余回调也通过独立的延迟调用得到执行。调用方应让回调有界，并允许它们在多次 `Go` 调用间并发执行。

采用 Go 1.25 的默认 panic 行为，`panic(nil)` 按 panic 报告。启用旧的 `GODEBUG=panicnil=1` 时，`recover` 返回 nil，该情形报告为未正常返回。回调持续阻塞时由调用方处置。

普通的空选项可省略。`nil` 闭包在新 goroutine 内按调用失败处理。上层继续负责取消、等待与重试策略。标准库提供恢复和堆栈能力。[Go 规范](https://go.dev/ref/spec#Handling_panics) · [堆栈](https://pkg.go.dev/runtime/debug#Stack)

## 验收

用有期限的同步信号观察正常返回、任务 panic、回调 panic 和不同调用的选项隔离。检查标准错误兜底、堆栈、回调顺序、耗时和退出状态，并运行 Go 竞态检查。按仓库的跨层约束核对生产代码的导入图。
