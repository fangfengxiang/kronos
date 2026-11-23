# 应用优雅重载

## 消费者与契约

以独立二进制运行的 Assistant 可以在同一组监听地址上更换进程。应用命令持有信号，`framework/app.RunWithReload` 接收重载通知、候选启动预算和可选 PID 文件路径。`SIGHUP` 触发一轮重载，`SIGINT` 或 `SIGTERM` 请求最终关停。重载模式适用于 Linux 和 macOS 的进程托管。容器编排可用部署滚动更新管理实例更替。

1. 当前进程保留所有已配置的 TCP 监听器。候选以相同的可执行文件路径和命令参数启动，继承监听器并重新装配服务。监听配置使用互异的地址作为继承键。
2. 候选的所有传输进入第一次 `Accept` 后，发布进程 PID 并确认接力。候选只在此后从监听器接收连接。旧进程撤销就绪并使用已有 `ShutdownTimeout` 排空已进入业务的调用。预算耗尽走传输的强制关闭路径。
3. 候选启动失败或超时，旧进程继续接流，失败由 `OnError` 暴露。一次只启动一个候选，接力后的进程等待上一代退出后再处理重载。最终关停会请求中止尚在启动的候选。

## 取舍与职责

沿用参考入口的显式信号处理与摘流顺序，以就绪反馈和共同的关停预算控制交接。Cloudflare tableflip 负责文件描述符继承、候选进程和就绪确认。Kronos 负责同一应用所有监听器的就绪门槛与关停。Kratos 负责各传输的排空。已有连接由接收它的进程收尾，后续连接可由新进程接收。[Cloudflare tableflip](https://pkg.go.dev/github.com/cloudflare/tableflip@v1.2.3) · [NGINX 进程交接](https://nginx.org/en/docs/control.html#upgrade)

监听器接力维持 TCP 监听队列。关停窗口内还未进入处理器的请求仍受传输的连接关闭语义约束，客户端可按原有幂等与重连规则处理。

候选可从原路径重新加载二进制和应用拥有的外部配置。升级二进制时，可先在同一文件系统构建候选，再原子替换原路径。PID 文件由部署方管理，包含最近一次发布就绪的进程 ID。

Assistant 候选启动时重新读取 `-config` 路径。文件读取或校验失败时，候选在装配监听器前退出，当前进程继续使用已加载的配置。候选成功就绪后发布新 PID。[启动配置文件](config-file.md)

## 平台支持

| 平台 | tableflip v1.2.3 与 Kronos 重载 | 仓库验收 |
| --- | --- | --- |
| Linux | 支持进程级重载，是首要部署平台。 | Linux amd64 交叉构建通过。Ubuntu CI 通过真实进程接力与在途流排空测试。 |
| macOS | 支持进程级重载。 | macOS arm64 真实进程接力与旧流排空通过。 |
| Windows | 上游不支持进程级重载。普通启动时省略 `-reload-pid-file`，启用重载会返回平台不支持错误。 | 上游声明与接入源码已核对。 |

[上游平台说明](https://pkg.go.dev/github.com/cloudflare/tableflip@v1.2.3#hdr-Overview)

## 通过条件

- 真实 Assistant 进程同时启动 HTTP、gRPC、SSE 和 go-micro v2。重载后 PID 变化，四个原地址可继续调用。发送最终 `SIGTERM` 后可重新绑定端口。
- 候选启动失败时观察到错误和旧进程仍服务，并可再次发起成功重载。用在途流检测旧进程在接力期间仍能完成已进入的工作。
- 模拟某个监听尚未进入 `Accept`，确认候选等待全部监听就绪后才接流。关停预算与启动回滚继续通过独立模块竞态测试。

## 实测与复盘

- 在 Go 1.25.0、macOS arm64 下运行 `bash scripts/test-modules.sh`。Assistant 子进程先启动一个退出码为 23 的候选，旧 PID 和四个端口继续服务。恢复可执行文件后成功接力。旧进程退出码为 0，新进程的四个入口可调用，最终 `SIGTERM` 后地址均可重新绑定。[Assistant 进程测试](../../examples/assistant/cmd/assistant/reload_test.go)
- 在 `framework/app` 内运行 `GOWORK=off go test -race -count=8 -run '^TestReloadDrainsAcceptedStream$' ./...`。候选等待第二个监听时，请求继续抵达旧进程。旧接收循环结束后，新进程接流，旧进程仍完成先前的 SSE 流。初次复测在接力边界观察到未进入处理器的 HTTP 请求返回 EOF，验收以两代接流状态和已进入的工作为观察点。[在途流进程测试](../../framework/app/reload_test.go)
- 使用 `GOWORK=off GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$CROSS_BUILD" ./examples/assistant/cmd/assistant` 生成 Linux 静态可执行文件。Ubuntu 的 `Go checks` 在 `f6b3505` 运行 `bash scripts/test-modules.sh`，通过四端口重载和在途流排空的真实进程测试。[Linux CI](https://github.com/fangfengxiang/kronos/actions/runs/45096952137)
