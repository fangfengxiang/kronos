# MySQL 连接池与文本后端配置

TextBackend 可以从 MySQL 读取选定的文本配置。业务用例接收一个能按请求上下文提供前缀的接口。应用设置 `-mysql-profile <name>`，并通过 `KRONOS_TEXTBACKEND_MYSQL_DSN` 提供连接信息。默认场景使用进程内的 `backend: ` 前缀。

启用 Redis 后，业务前缀源按 TTL 在缓存与 MySQL 间读取。[Redis 缓存](redis-text-profile-cache.md)

## 契约与所有权

- `infra/mysql` 是独立 Go 模块。`Open` 取得一个标准库 `*sql.DB`，在调用者上下文内完成 `PingContext`。失败释放连接资源，已成功的池由调用方关闭。连接上限、空闲上限和最大存活时间沿用 `database/sql`，零值采用标准库默认设置。
- 建池参数错误返回 `ErrInvalidConfig`，连通性或认证失败返回 `ErrUnavailable`，上下文取消和超时保留标准语义。建池错误可以安全写入日志。`IsDuplicateKey` 识别 MySQL `ER_DUP_ENTRY`，包括经 `%w` 包裹的错误。
- TextBackend 读取 `text_backend_profiles` 的 `output_prefix`。部署方应用 [SQL 表定义](../../examples/textbackend/schema/mysql.sql) 并写入选定配置。启动时检查池和配置可用，每次生成时用绑定参数读取当前前缀。配置缺失或查询失败时，RPC 沿用 `ERROR_REASON_GENERATION_FAILED`。结束接流以后关闭数据库池。表和配置记录由部署方管理。

## 复用与取舍

参考 DAO 经配置获取连接池，按业务对象执行查询。参考 MySQL 模块提供启动探活、池设置、查询、事务和重复键识别，GORM 模块用于部署中的 ORM DAO。当前查询只有一个参数化主键，沿用 Go 标准库 `QueryRowContext`、`ExecContext`、`BeginTx` 和 `sql.ErrNoRows`，由官方 MySQL driver 实现协议。GORM 自身也可以复用同一个标准池，后续 ORM 消费者可以独立装配。[Go `database/sql`](https://pkg.go.dev/database/sql@go1.25.0) · [Go MySQL Driver v1.10.2](https://github.com/go-sql-driver/mysql/tree/v1.10.2) · [GORM 已有连接](https://gorm.io/docs/connecting_to_the_database.html#Existing-database-connection) · [MySQL `ER_DUP_ENTRY`](https://dev.mysql.com/doc/mysql-errors/8.4/en/server-error-reference.html#error_er_dup_entry)

## 通过条件

- 原生 Go 测试验证参数校验、取消握手和失败释放。错误输出使用稳定语义，连接参数由调用方持有。
- 在 Ubuntu 使用官方 MySQL 测试实际建池。与标准 `sql.Open` 比较查询结果和事务回滚，验证上下文取消、池恢复及重复键识别。提供可运行的 `ExampleOpen`。
- 启动真实 TextBackend，凭配置从 MySQL 查询 Unicode 前缀。更新数据库后同一进程可读到新值。空配置记录返回稳定 RPC 错误。`SIGTERM` 后应用退出，运行验收和目标平台构建结果分别记录。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 上执行 `GOWORK=off go -C infra/mysql test -tags=mysql_integration -count=3 -v ./...`，使用 `KRONOS_MYSQL_TEST_DSN` 指向独立测试库。服务端为官方 MySQL 8.4.13，镜像摘要为 `sha256:a81d043930e51745522bd5c430961cf8286f0a23f0a9a2a6e0108f7ea71b0902`。两种池读取相同 Unicode 结果，连接 ID 相异，事务均可回滚，查询取消后均恢复可用。重复键读取稳定的错误类别。[Go 对照与示例](../../infra/mysql/integration_test.go) · [官方镜像版本](https://github.com/docker-library/official-images/blob/master/library/mysql)
- 对照中，原生 `sql.Open` 可以在密码错误时返回一个池，`PingContext` 才报告 `1045`。Kronos 在启动探活时返回 `ErrUnavailable` 和空池。取消尚未收到服务端握手的连接时观察到对端 EOF。生产命令的直接导入仅使用标准库和 Kronos 模块。[建池和握手测试](../../infra/mysql/pool_test.go)
- 在同一 Linux 环境执行 `GOWORK=off go test -tags=mysql_integration -count=3 -v ./test/integration/mysql`，三轮通过。真实进程读取初始及更新后的前缀，删除配置后返回 `ERROR_REASON_GENERATION_FAILED`。`SIGTERM` 正常退出，缺失配置时启动失败，端口可重新绑定。[文本后端实测](../../test/integration/mysql/profile_test.go)
- macOS arm64 的全模块竞态、分层依赖检查和 Linux Compose 默认部署回归通过。`infra/mysql` 和 TextBackend 通过 Windows amd64 交叉构建，运行验收在 Linux 完成。
