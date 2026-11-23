# ADR 0009 注册发现与协议地址

**状态。** 采纳 etcd v3 租约与按协议分开的实例地址。

## 背景

同一个服务可以在独立端口提供标准 gRPC 与 go-micro v2 一元 RPC。服务部署者配置统一的发现名，业务契约继续由 Protobuf 定义。旧客户端通过官方 etcd Registry 获取节点，其选择器根据节点地址发起调用。

## 决定

- go-micro v2 节点使用 `/micro/registry/<服务名>/<实例 ID>`，每个键保存一个官方 Registry 可读取的 JSON 服务版本和节点地址。
- 标准 gRPC 节点使用 `/kronos/registry/grpc/<服务名>/<实例 ID>`，值为该监听的地址。客户端按显式协议读取相应前缀。默认选择 gRPC。
- 单次发布使用同一实例 ID 和 etcd 租约。双端口地址通过一次事务写入。发布者维持租约，经过确认的过期事件触发重新发布。撤销租约移除两项地址。进程意外终止时由 etcd 回收租约。
- 应用负责判断监听就绪、启动发布和发起摘流。etcd 适配器负责地址编码、租约管理和读取。调用者通过依赖注入交接注册实例与发现能力。

## 取舍

在同一旧协议节点集合写入两种地址会让原有客户端把调用拨到 gRPC 端口。协议分区使旧客户端和新客户端都能用自己的发现规则。独立注册命名空间也允许新部署滚动接入。兼容行为由固定版本官方客户端和服务器在真实 etcd 上复验。[go-micro v2.9.1 Registry](https://github.com/micro/go-micro/blob/v2.9.1/registry/etcd/etcd.go) · [etcd 租约与事务](https://etcd.io/docs/v3.6/learning/api/)

生产代码复用官方 etcd v3 Go 客户端。go-micro 依赖留在隔离测试夹具。上下游交互、超时、回滚和验收证据见[服务治理设计](../design/service-governance.md)。
