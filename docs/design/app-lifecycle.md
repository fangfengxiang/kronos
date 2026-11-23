# 应用装配与关停

## 消费入口

Assistant 命令入口显式提供 HTTP 与 gRPC，按配置启用 SSE 与 go-micro v2 兼容监听，并持有 `SIGINT`、`SIGTERM` 的订阅。启用重载时另订阅 `SIGHUP`。`framework/app` 接收标准上下文、监听地址及关停预算。业务实现通过 `Service.Run` 提供一元调用，通过 `StreamService.StreamRun` 提供流式调用。Kratos 在私有适配内负责服务循环、标准传输与连接关停。[SSE 与 go-micro v2 监听](additional-listeners.md) · [优雅重载](graceful-reload.md)

Assistant 命令在创建应用组件前读取并合并可选的启动配置，文件与参数的解析错误沿命令入口返回。[启动配置文件](config-file.md)

独立命令进程在启动、候选失败、退出和业务处理完成时写入 JSON 日志。HTTP 与 gRPC 传输的启动和关停消息使用同一日志输出。[结构化日志](structured-logging.md)

## 运行行为

1. 每个 `App` 实例运行一次。先按声明顺序取得所有 TCP 监听器，再用它们构造服务。应用持有监听器和成功构造的服务。构造未完成时，应用限时逆序停止已交接的服务，并释放监听器。失败的构造函数负责清理尚未交接的资源。首因和清理错误由 `Run` 返回。
2. 全部服务进入第一次 `Accept` 后发布就绪并开始接收连接。探针通过标准 HTTP handler 提供 `/livez` 和 `/readyz`，Assistant 将它们注册在自己的 HTTP 监听器上。业务调用在入口读取就绪状态，通过检查后才交给用例。
3. 停止信号撤销就绪，所有已启动服务使用同一个有截止时间的上下文并行关停。应用给在途请求保留原本的运行上下文。预算耗尽时取消服务上下文，Kratos 负责强制关闭连接，应用释放监听器。`Run` 保留服务失败与清理失败。原生监听器因正常关停已关闭时，重复清理视为成功。

## 服务发布

通用服务装配使用 `RunWithReadyHook`。当全部监听已进入接收循环、`Ready()` 可用后，应用将实际监听名称和地址交给一次性钩子。钩子可用启动上下文发布服务。成功取得的资源通过返回的撤销函数交给应用，返回错误会触发启动回滚。钩子将部分完成的资源交给撤销函数，或在返回前完成自身清理。

应用停止先撤销就绪。已交接的撤销函数执行完毕后，传输开始排空。撤销与传输使用同一个有截止时间的关停上下文。发布与撤销函数遵守其上下文期限。调用方通过就绪决定是否接纳新的业务请求。标准 `Run` 沿用原入口，注册中心由消费方注入。[服务治理契约](service-governance.md)

## 复用取舍

- 参考服务真实入口将依赖初始化、注册及运行放在一处，类库按声明顺序初始化并在首个错误处停止。沿用显式装配、顺序取得资源和单次关停。参考测试以启动调用和定时等待为主。本切面用端口重绑与真实请求观察就绪和释放。
- 直接复用 Kratos HTTP/gRPC 的 `Start`、`Stop` 与超时后强制关闭。Kratos v3.0.0 的 `App.Run` 用 goroutine 调度作为启动观测，并在内部注册进程级信号。Kronos 只协调监听器所有权与就绪、摘流。信号由命令入口通过 `signal.NotifyContext` 持有和释放。停止请求与在途工作的上下文相互独立。[Kratos 应用实现](https://github.com/go-kratos/kratos/blob/v3.0.0/app.go) · [Go 信号上下文](https://pkg.go.dev/os/signal#NotifyContext)
- 当前探针与 HTTP 应用共用监听器。摘流时调用方可能观察到就绪撤销或监听器关闭。把服务发现和容器发布接入同一状态是后续消费者切面。

## 验收

- 占用第二个端口后触发真实启动失败，第一个端口可以立即重新绑定，实例没有发布就绪。构造和运行失败也能触发清理。
- 用一元 HTTP 和 gRPC 真实调用验证探针、共享用例及请求拒绝。开始一个在途请求后触发停止，覆盖预算内完成和预算到期后取消。
- 关闭工作区构建 Assistant 命令并启动真实子进程，从四个入口调用后发送 `SIGTERM`，检查退出状态与端口释放。分别运行 `framework/app` 包的竞态检查和仓库跨模块检查。
- 在真实回环监听上由发布钩子发起 HTTP 请求，确认监听可用，再撤销就绪。撤销和传输停止使用相同的截止时间，传输停止发生在撤销之后。模拟发布失败，观察错误聚合和可重新绑定的监听端口。

### Order 双协议关停

- 用一个 Order 实例同步接纳 gRPC 和 MicroRPC 请求，把两个业务处理都留在途。真实注销开始时暂停 etcd 撤销，验证两种协议仍能发现端口，但新请求收到 `SERVICE_UNAVAILABLE`，共享的业务实现只接纳先前两次调用。完成注销后允许在途请求正常返回，随后验证两端口均能重新绑定。
- 让在途处理留到关停期限耗尽，验证 `Run` 返回期限错误，两种协议的请求和业务上下文均结束，两个端口均释放。用调用方显式取消与超时独立验证两种协议传递 Deadline 与 Cancel。继续使用 Kratos 的正常和强制关停能力。[gRPC 优雅关停](https://grpc.io/docs/guides/server-graceful-stop/) · [gRPC 期限](https://grpc.io/docs/guides/deadlines/) · [gRPC 取消](https://grpc.io/docs/guides/cancellation/)

## 实测与复盘

- 首次验收使用 Go 1.25.0，全仓测试、分层检查和协议生成一致性通过。根模块及 `framework/app` 的 `GOWORK=off go vet ./...` 通过。当前使用仓库的原生 Go 验收入口。[验收命令](go-verification.md)
- 模块测试验证第二个端口被占用后的重绑、已构造服务的失败回滚、错误聚合和探针状态。真实 HTTP、gRPC 的在途请求完成与超时取消用例在竞态检测下重复八次通过。
- Assistant 子进程测试构建并启动命令，探针就绪后通过四个入口发起请求，发送 `SIGTERM` 后观测到退出码为零，四个端口均可重新绑定。容器、注册发现与模型接入由各自的纵切验收。
- Go 1.25.0、Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C framework/app test -count=3 ./...` 和 `GOWORK=off go test -count=1 ./examples/assistant/...`，均通过。macOS arm64 同组验证加 `-race` 通过。新测试在发布钩子内成功请求实际绑定的端口，观察到撤销先于传输停止且两者期限一致。部分发布失败后同时保留发布和撤销错误，端口可继续绑定。[发布钩子回归](../../framework/app/ready_test.go)
- 同一 Linux 环境与真实 etcd v3.6.16 运行 `GOWORK=off go test -tags=interop,etcd_interop -count=4 -run 'TestOrder(Withdraws|Shutdown|Caller)|TestDiscoveredOrder' -v ./test/interop/microv2`。两种协议在摘流窗口拒绝新请求，处于在途状态的两个调用在真正注销后完整返回。另行验证单秒关停预算、监听可重新绑定、客户端 Deadline 传入处理器以及显式取消终止处理。[双协议生命周期测试](../../test/interop/microv2/drain_test.go)
