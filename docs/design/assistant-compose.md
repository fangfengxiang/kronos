# Assistant 容器交付

## 消费者与契约

开发者从仓库根目录使用 Docker Compose 启动确定性模型的 Assistant。Linux 镜像直接执行 Assistant 进程，Compose 挂载只读 JSON 配置。HTTP、gRPC、SSE 和 go-micro v2 分别监听容器的 `8080`、`9090`、`8081`、`9091`，宿主机仅回环地址按需分配端口。客户端用 `docker compose port assistant <容器端口>` 查询。

- 应用进程以非 root 用户运行。Compose 使用 HTTP `/readyz` 判定就绪，`/livez` 供存活检查。进程标准错误流记录结构化启动、请求和退出事件。
- Compose 附带配置启用 `/metrics`。指标经同一个宿主回环地址抓取。[请求指标](request-metrics.md)
- `docker compose stop` 向主进程发送 `SIGTERM`。容器留 10 秒给停止流程，配置中的应用关停预算为 5 秒。实际进程排空规则见应用生命周期与流测试。[应用装配与关停](app-lifecycle.md)
- 配置读取失败时进程以非零退出码结束。部署方可以从日志定位配置路径和错误。镜像和确定性模型运行无需外部模型凭据。

## 边界与取舍

- 参考服务使用多阶段构建，并用 `exec` 将应用提升为容器主进程。Kronos 沿用两阶段交付，Docker 直接执行 Go 二进制。构建阶段使用官方 Go 镜像，运行阶段使用 Alpine 自带的 `wget` 执行容器探针，保留同一个 HTTP 就绪契约。[Docker 多阶段与入口](https://docs.docker.com/reference/dockerfile/#entrypoint) · [Alpine 官方镜像](https://hub.docker.com/_/alpine)
- Compose 管理配置、端口、健康状态及停止信号。测试以 Go 客户端和标准库直接验证公开入口。框架继续负责监听取得、就绪和业务排空。[Compose 服务定义](https://docs.docker.com/reference/compose-file/services/) · [Compose 配置文件](https://docs.docker.com/reference/compose-file/configs/) · [Compose 等待就绪](https://docs.docker.com/reference/cli/docker/compose/up/)

## 通过条件

- `go test -tags=e2e -count=1 ./test/e2e/assistant` 使用独立项目构建镜像、等待健康、验证探针与四个真实入口。
- 用同一镜像触发配置文件缺失和监听端口冲突，观察失败退出和记录原始错误的结构化事件。
- 停止运行容器，确认退出码为零、日志包含正常退出事件，宿主机已停止接流。诊断在清理测试项目之前采集。

## 实测与复盘

- 使用 Go 1.25.0、Docker Compose v5.7.0 在 Linux arm64 引擎中实际构建和启动镜像。`GOWORK=off go test -race -tags=e2e -count=2 -v ./test/e2e/assistant` 两轮通过。`/livez` 和 `/readyz` 正常，三个一元入口返回中文响应和稳定错误码，SSE 两次增量合并为中文正文并有唯一完成事件。[Go 容器测试](../../test/e2e/assistant/compose_test.go)
- 配置缺失和第二个监听端口冲突均以退出码 1 结束，并输出带原因的 `app_failed`。Compose 停止后容器以退出码 0 结束，出现 `app_stopped`，四个宿主端口停止接流。复盘补上端口冲突，覆盖容器内取得首个监听后出现的启动失败。在途请求排空和超时取消由应用模块的进程及真实传输测试验证。[关停验收](app-lifecycle.md#实测与复盘)
- 构建镜像为 `golang:1.25.0-alpine3.22`，运行镜像为 `alpine:3.22.8`，均按多架构摘要固定。Ubuntu CI 使用同一个 Go 测试入口。[容器构建](../../examples/assistant/deploy/Dockerfile) · [CI](../../.github/workflows/ci.yml)
