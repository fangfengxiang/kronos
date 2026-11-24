# 统一应用的一元 HTTP 监听

Order 运维者和 HTTP 调用方使用同一个服务定义。应用只增加 `kronos.HTTP(address)`，`google.api.http` 注解决定路由，生成的 `BindOrderService` 继续注册原来的业务实现。Order 通过 `KRONOS_ORDER_HTTP` 启用该监听，空值禁用。

首个切面支持静态路径的 GET 和整条 Proto 消息作为请求体的 POST。GET 使用 Kratos 的 query 绑定；POST 使用请求体，查询参数不覆盖正文。Kratos 负责路由、query 绑定和错误编码，消息正文使用 ProtoJSON，POST 输入限定 1 MiB。首轮运行观察到 Kratos 的普通 JSON codec 忽略了 `orderId`，因此为该监听显式使用 Protobuf 的 JSON 映射。解码失败以稳定的 `INVALID_REQUEST` 响应。方法只在显式注解后对 HTTP 可见。启用监听时，重复路由、空映射和暂不支持的模板、字段映射、多重绑定应在开始接纳请求前报错。HTTP 一元的服务端预算为 30 秒，调用端断开会取消业务上下文。[Kratos HTTP Context](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/http/context.go) · [Google HTTP 映射](https://github.com/googleapis/googleapis/blob/master/google/api/http.proto)

HTTP 请求直接进入已有的服务绑定。POST 使用 `Content-Type: application/json`，其他请求类型返回 415 和 `UNSUPPORTED_MEDIA_TYPE`。请求 ID、语言、W3C 链路、稳定错误、就绪和摘流沿用统一应用机制。Prometheus 为已登记方法提供 `http/server` 标签。HTTP 与诊断监听由同一 App 管理，etcd 和 Consul 仍发布现有 RPC 协议地址。单独运行 HTTP 时使用显式监听地址，启用注册中心需要至少一个 RPC 监听。[ProtoJSON 格式](https://protobuf.dev/programming-guides/json/)

参考接入将网关与 RPC 服务独立装配，网关转发到同进程的 RPC 地址。Order 直接复用 HTTP 路由和已有业务绑定可省去第二次编解码、查址和回环调用。后续 SSE 用独立的流终止契约接入。回退只需取消配置 HTTP 监听。

Linux 验收启动真实 Order 和 etcd，验证同一 Unicode 订单经过 gRPC、MicroRPC、HTTP 得到相同结果。检查非法业务输入、稳定的解码失败、HTTP 指标及其请求 ID 和 Trace、关闭后的端口回收。覆盖纯 HTTP、不支持的映射及重复路由的配置边界。

## 实测

- Go 1.25.0、Ubuntu 24.04 Linux arm64、官方 etcd v3.6.16。以 `GOWORK=off go test -tags=etcd_interop -count=3 -run '^TestOrder(CorrelatesSignalsAcrossTransports|WithdrawsBeforeBothProtocolsDrain|HTTPCancellationReachesBusiness)$' -v ./test/interop/microv2` 三轮通过。同一订单经三路返回，HTTP 失败日志与指标及 OTLP 父级对应。畸形、超限与非 JSON 请求返回约定错误，查询参数无法覆盖正文。HTTP 断开到达业务上下文；摘流拒绝新请求，已接纳的请求完成，随后四个监听端口均释放。[调用和信号](../../test/interop/microv2/trace_test.go) · [取消与排空](../../test/interop/microv2/drain_test.go)
- 相同 Linux 环境的统一应用、Prometheus 及 `go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2` 通过默认回归，独立负载及私有证书场景按环境变量另启。两个旧版官方客户端仍能双向调用。macOS arm64 的全仓竞态、分层、重新生成、相对 `7c29b84` 的兼容检查通过。外部包编译断言覆盖新增选项，模块测试覆盖纯 HTTP 构造、GET 解码、重复及不支持的映射。[公开签名](../../platform/kronos/public_test.go) · [路由边界](../../platform/kronos/internal/transport/http_test.go)
