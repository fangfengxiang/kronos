# MongoDB 类型化文档与文本配置

TextBackend 可以从 MongoDB 集合读取选定的文本配置。应用使用 `-mongo-profile <name>` 和 `KRONOS_TEXTBACKEND_MONGODB_URI`，数据库由 `-mongo-database` 指定，默认 `kronos`。应用入口一次选择一种文本配置来源。[MySQL 配置](mysql-text-profiles.md) · [OSS 配置](oss-text-profiles.md)

## 契约与所有权

- `infra/mongodb` 独立管理官方 Go driver 的连接。`Open` 接收 URI 与数据库名称，按调用者上下文等待 SRV 配置解析和探活。等待期间上下文结束时返回对应的 `context.Canceled` 或 `context.DeadlineExceeded`。连接配置要求写入确认，显式的未确认模式返回 `ErrInvalidConfig`。启动失败时清理内部连接。成功返回的 `Database` 由调用方以独立关停上下文关闭。[MongoDB 写入确认配置](https://www.mongodb.com/docs/drivers/go/current/crud/configure/)
- `NewCollection[T]` 将 Go 文档结构映射到具名集合。公开能力为 `FindByID`、`ReplaceByID` 和 `DeleteByID`，以字符串 `_id` 标识文档。替换使用原子 upsert，文档可以省略 `_id`，显式字段须与方法参数相同。删除返回是否实际移除。驱动的客户端、集合、结果与 BSON 查询对象由适配层持有。配置、缺失、冲突和连接失败使用稳定的包错误，取消与超时保留 Go 上下文语义。[MongoDB 替换规则](https://www.mongodb.com/docs/manual/reference/method/db.collection.replaceone/)
- TextBackend 使用 `text_backend_profiles` 集合中的 `output_prefix` 字符串，读取当前值。空字符串是有效前缀，缺失或 null 的字段是配置错误。服务启动时检查所选配置，运行中配置缺失或读取失败返回公开的 `ERROR_REASON_GENERATION_FAILED`。启动、读取和关闭分别使用调用方传入的期限。

## 复用与取舍

参考业务用驱动 `FindOne` 解码领域结构，并用 `UpdateOne` 的 upsert 保存状态。参考装配层持有连接配置，业务 DAO 持有查询。当前切面沿用独立连接、类型化解码与原子写入，以具名配置文档验证单条读取、替换和删除。选择兼容 Go 1.25.0 的官方 driver v2.9.2，直接使用它的驱动、编码和错误分类。公开 API 保留实际消费的文档操作和 `_id` 语义。[MongoDB Go driver v2.9.2](https://github.com/mongodb/mongo-go-driver/releases/tag/v2.9.2) · [上下文与取消](https://www.mongodb.com/docs/drivers/go/current/context/) · [类型化 BSON 解码](https://www.mongodb.com/docs/drivers/go/current/data-formats/bson/)

v2.9.2 的 `ApplyURI` 同步查询 `mongodb+srv` 的 TXT 和 SRV 记录，公开接口无上下文参数。继续由驱动解析原 URI，保留 TXT 选项、TLS 默认值、主机校验及拓扑发现。可取消的 SRV 启动把配置解析交给独立任务，调用者可以结束等待。取消后，已经发出的 DNS 查询按原解析器的期限收尾，任务只产生配置结果，无权创建客户端。常规 `mongodb://` 使用同步配置路径。[锁定版本的 URI 处理](https://github.com/mongodb/mongo-go-driver/blob/v2.9.2/mongo/options/clientoptions.go) · [SRV 格式](https://www.mongodb.com/docs/manual/reference/connection-string-formats/#srv-connection-format)

## 通过条件

- 在 Ubuntu 对照官方 driver 验证写入与读取的 BSON 对等、替换、重复唯一值、缺失和删除。验证连接失败回收、取消和可重复关闭，并提供原生 Go `Example`。
- 用官方 MongoDB 启动真实 TextBackend 进程。验证 Unicode 配置更新、空字符串、运行时字段或记录缺失的稳定 RPC 错误、`SIGTERM` 释放端口以及启动时缺失记录的诊断。
- 验证独立模块和应用编译、跨层导入及目标平台。记录 Linux 运行结果和 Windows 构建证据。
- 用 Go 测试在驱动 TXT 查询期间分别取消和触发期限。解析夹具仍被阻塞时，`Open` 已返回正确的上下文错误且未返回数据库。释放查询后任务退出。常规 URI 的非法配置与 TCP 连接取消继续通过。

## 实测记录

- Go 1.25.0、Ubuntu 24.04 Linux arm64 连接隔离网络中的官方 MongoDB 8.0.38，镜像摘要为 `sha256:479e85ced54ffb8a40dade1dcde2da2a9ae858515b9415e9cc54bfb8f0575d01`。设置 `KRONOS_MONGO_TEST_URI` 指向测试数据库，`KRONOS_MONGO_TEST_DATABASE` 为空时使用 `kronos_test`。运行 `GOWORK=off go -C infra/mongodb test -tags=mongodb_integration -count=3 -v ./...`，官方 driver 对照、重复唯一值、取消、关闭及 Go `Example` 通过。[对照测试](../../infra/mongodb/integration_test.go) · [MongoDB 官方镜像](https://github.com/docker-library/official-images/blob/master/library/mongo)
- 同环境运行 `GOWORK=off go test -tags=mongodb_integration -count=3 -v ./test/integration/mongodb`，三次真实进程调用均通过。服务读取 Unicode 配置及后续更新，接受空前缀，返回约定的缺失错误，关停后端口可复用。缺失配置时新进程在启动阶段退出。[进程集成测试](../../test/integration/mongodb/profile_test.go)
- `go run ./platform/verify deps`、根模块与适配模块的 `go mod tidy -diff` 和 `go vet -tags=mongodb_integration ./...` 均通过。`CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go -C infra/mongodb build ./...` 和相同目标的 TextBackend `go build` 通过。以上 Windows 结果为构建证据，运行验收在 Linux 完成。
- macOS arm64 运行 `go run ./platform/verify test` 通过各独立模块的竞态测试。运行 `GOWORK=off go test -race -tags=e2e -count=1 -v ./test/e2e/assistant` 通过 Assistant 默认 Linux Compose 部署回归。
- 修复前，新增 SRV 用例的取消和期限两项都要等到夹具释放 DNS 查询。Go 1.25.0、Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C infra/mongodb test -count=5 ./...` 通过，macOS arm64 同项以 `-race` 五轮通过。另在隔离网络中使用本地官方 MongoDB 8.0.38 镜像运行 `GOWORK=off go -C infra/mongodb test -tags=mongodb_integration -count=1 ./...` 和 `GOWORK=off go test -tags=mongodb_integration -count=1 ./test/integration/mongodb`，直连驱动对照与真实 TextBackend 进程回归通过。[SRV 取消用例](../../infra/mongodb/srv_test.go)
