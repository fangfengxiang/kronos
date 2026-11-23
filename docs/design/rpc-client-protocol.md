# RPC 客户端协议选择

## 消费者与接口

服务调用方在装配 `assistant.NewRPCClient(ctx, endpoint, timeout, options...)` 时选择协议。省略选项使用标准 gRPC。配置为 `rpc.WithProtocol(rpc.ProtocolMicroV2)` 时，使用 go-micro v2 的一元 `application/proto-rpc`。选项可在不同实例中独立设置，重复指定时以最后一个为准。未知值返回 `rpc.ErrUnsupportedProtocol`。

业务继续使用同一份 Protobuf 请求与响应，并通过 `Run`、`Close` 与公开的 `rpc.Error` 工作。客户端保留调用方期限。没有调用方期限时，沿用现有默认期限规则。关闭后的新调用由共享一元客户端拒绝。

## 复用与边界

参考调用在装配边界确定连接配置，并把类型化客户端交给业务。沿用这条边界。`framework/rpc` 管理协议选项与现有调用行为。标准 gRPC 通过已有 Kratos 绑定。`infra/microv2` 复用兼容监听的帧编码，负责直连 HTTP、调用标识、纳秒期限、有限响应读取及远端错误解析，供应用私有绑定装配。旧 SDK 用于独立互通验收。[旧 Protobuf 信封](https://github.com/micro/go-micro/blob/v2.9.1/codec/protorpc/envelope.proto) · [go-micro v2 服务端编码](https://github.com/micro/go-micro/blob/v2.9.1/server/rpc_codec.go)

旧客户端使用 `host:port` 或 HTTP(S) 地址，目标服务和方法来自装配。Assistant 的目标服务为 `kronos.assistant.v1.Assistant`，方法为 `Assistant.Run`。请求与响应各限 1 MiB，旧客户端按响应序号关联调用。官方旧服务端的业务错误帧可能将序号置零，当帧内序号为零且存在业务错误时，以响应的 `Micro-Id` 关联原请求。协议在创建时固定，每次调用只走选定的传输。

## 通过条件

- 同一个进程同时开启两种 RPC 监听。省略协议选项和显式 gRPC 均调用标准监听，显式旧协议调用兼容监听。成功结果、无效提示词、上下文取消及关闭语义一致。
- 独立测试核对旧帧、服务和方法头、超时头及响应序号错误。用官方旧服务端核对 Kronos 客户端的一元结果和公开错误。

## 实测与复盘

- Go 1.25.0 下 `bash scripts/test-modules.sh` 通过。真实监听验证默认标准 gRPC、显式标准 gRPC 和旧协议选项。取消传播、关闭语义、错误转换与旧协议边界分别进入包级和参考应用测试。
- 运行 `bash test/interop/microv2/verify.sh "$MICRO_V2_ADDRESS"`，与官方 `v2.0.0`、`v2.9.1` 双向验证 Unicode 响应和稳定业务错误。旧服务端的错误帧零序号由匹配的 `Micro-Id` 补足。旧 SDK 仅用于互通夹具，依赖清单在临时模块中适配 Go 1.25。[互通脚本](../../test/interop/microv2/verify.sh)
