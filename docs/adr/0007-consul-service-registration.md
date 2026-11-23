# ADR 0007 Consul 实例注册与健康地址订阅

**状态。** 采纳独立 Consul 适配模块，由 TextBackend 启动入口装配。

TextBackend 通过独立进程提供一元 gRPC。部署方需要按逻辑名称定位健康实例，并在进程停止接流前撤销实例登记。实例 ID 由进程装配生成，每代拥有独立 ID。公布地址来自实际 TCP 监听，部署方可为跨网络访问提供可达地址。

采用官方 `github.com/hashicorp/consul/api` v1.32.4 保持 Go 1.25.0 基线。Kratos v3.0.0 的 Consul 扩展要求 Go 1.26，对应观察实现只广播非空更新。Kronos 需要保留最后一个健康实例移除的空快照，以便消费方清除旧地址。官方 SDK 的健康查询和 service watch 满足此要求，处理阻塞查询、索引回退、重试与取消。Kratos 继续提供 gRPC 服务端及标准健康检查。[Consul SDK v1.32.4](https://github.com/hashicorp/consul/tree/api/v1.32.4/api) · [Kratos Consul 模块](https://github.com/go-kratos/kratos/blob/v3.0.0/contrib/registry/consul/go.mod) · [Kratos 观察实现](https://github.com/go-kratos/kratos/blob/v3.0.0/contrib/registry/consul/registry.go)

应用掌握注册生命周期。适配包公开实例和地址等值类型，Consul SDK 类型留在实现。健康检查通过后实例进入发现结果，停止时尽力注销并继续排空。首个切面的范围和通过条件见[Consul 服务注册设计](../design/consul-service-registration.md)。
