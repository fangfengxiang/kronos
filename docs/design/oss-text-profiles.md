# OSS 对象与文本配置

TextBackend 可读取 OSS 中的一份 UTF-8 文本作为前缀。部署者从应用入口选择 `-oss-profile <object-key>`，并提供桶、区域与访问凭据。每次 RPC 读取选定对象的当前内容。

## 契约与所有权

- 独立模块 `infra/aliyunoss` 的 `New` 接收区域、桶、可选 `http(s)` endpoint 与一组显式凭据。默认使用区域 HTTPS endpoint 和 V4 签名。专属域名用 `UseCNAME` 声明，STS 临时凭据可传 `SessionToken`。每个桶对象持有独立客户端和连接池，调用方可在停流后释放空闲连接。
- `Open` 返回由调用方关闭的 `io.ReadCloser`，其读取使用传入的上下文。`Put` 使用 `io.ReadSeeker` 从当前位置直接上传，保留输入的关闭责任。`PresignGet` 生成有效期为整秒且在 1 秒至 7 天范围内的 V4 临时下载 URL。STS 令牌可以更早过期。三种操作使用同一桶和未经规整的 UTF-8 对象键。[对象键规则](https://www.alibabacloud.com/help/en/oss/user-guide/object-overview) · [限时 URL](https://www.alibabacloud.com/help/en/oss/developer-reference/v2-presign-download)
- 缺失对象、访问被拒、错误配置和非法参数有稳定的包错误。桶缺失及其他 OSS、协议或连接错误返回 `ErrUnavailable`。取消和超时保留 Go 上下文语义，下载流的读取与关闭返回标准 I/O 错误。入口日志可使用适配错误描述，SDK 诊断日志保持关闭。
- TextBackend 在监听前限时读取对象验证配置。请求上下文继续约束后续下载。对象内容最多 4096 字节，空文件是有效前缀，格式错误、读取失败或对象消失通过现有 `ERROR_REASON_GENERATION_FAILED` 报告。应用装配在关停时回收空闲连接。内置前缀、MySQL、MongoDB 和 OSS 分别由同一个应用入口选择。

## 复用与取舍

参考业务从对象键读取文件、用同一个客户端上传内容，并为私有内容生成临时下载 URL。其上游由业务进程装配。当前封装保留按桶建客户端、按对象键读写、独立生成下载授权这三项操作，并复用 OSS Go SDK V2 的 HTTP 传输、V4 签名、请求重试和上传校验。服务端持有写权限时直接使用 `PutObject`。本切面的对象内容由部署者整份更新，公开接口由调用路径所需的操作确定。[OSS SDK V2](https://www.alibabacloud.com/help/en/oss/developer-reference/manual-for-go-sdk-v2/) · [官方上传说明](https://www.alibabacloud.com/help/en/oss/developer-reference/v2-simple-upload)

所选 SDK v1.6.1 支持 Go 1.18 及以上。在共享客户端上关闭 SDK 的时钟偏移自动修正，因为该版本在请求之间无同步地保存偏移。签名依赖宿主时钟同步，其余 SDK 重试和检查沿用默认值。[SDK v1.6.1](https://github.com/aliyun/alibabacloud-oss-go-sdk-v2/releases/tag/v1.6.1) · [时钟偏移实现](https://github.com/aliyun/alibabacloud-oss-go-sdk-v2/blob/v1.6.1/oss/client.go)

## 通过条件

- 在 Linux 用本地 HTTP 协议夹具与官方 Go 客户端对照，验证 Unicode 键、上传与下载、STS 请求头、签名下载查询、独立桶、缺失和权限错误、取消及资源关闭。并发请求验证在收到服务端时钟错误时偏移保持独立于响应。
- 在 Linux 启动真实 TextBackend，确认同一进程可反映更新与空对象，读不到有效配置时返回稳定错误，进程关停后端口可复用。应用启动要求所选对象可读且内容有效。
- 提供原生 Go `Example`，核对独立模块和应用依赖。分别记录 Linux 运行范围和 Windows 构建结果。

## 实测记录

- Go 1.25.0、Ubuntu 24.04 Linux arm64 执行 `GOWORK=off go -C infra/aliyunoss test -count=3 -v ./...`。签名请求、Unicode 对象键、实际 HTTP 读写、签名下载、错误分类、流取消和 Go `Example` 通过。测试使用本地 OSS HTTP 协议夹具，覆盖 SDK 与适配行为。线上 OSS 账号的权限、桶及域名配置待部署环境验收。[原生 SDK 对照](../../infra/aliyunoss/bucket_test.go)
- 同环境执行 `GOWORK=off go test -count=3 -v ./test/integration/oss`。真实 TextBackend 三轮通过，覆盖更新、空对象、过大及非法 UTF-8 内容、对象缺失、启动失败、日志凭据检查和 `SIGTERM` 端口回收。[进程集成测试](../../test/integration/oss/profile_test.go)
- macOS arm64 的 `GOWORK=off go -C infra/aliyunoss test -race -count=3 -v ./...` 通过。临时 Go `-overlay` 保留 SDK 默认时钟修正时，模拟时钟错误使回归测试检测到超前的请求时间。仓库版本使用同一测试通过。`go run ./platform/verify test`、`go run ./platform/verify deps`、根模块及适配的 `go mod tidy -diff` 和 `go vet ./...` 均通过。默认 Assistant Linux Compose 回归运行 `GOWORK=off go test -race -tags=e2e -count=1 -v ./test/e2e/assistant` 通过。
- `GOWORK=off CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go -C infra/aliyunoss build ./...` 及相同目标的 TextBackend `go build` 通过。Windows 只记录构建证据，运行结果来自上述 Linux 和 macOS 验收。
