# Redis 字符串缓存与文本配置回源

TextBackend 的 MySQL 文本前缀可用 Redis 缓存。部署方用 `-profile-cache-ttl` 启用，并通过 `KRONOS_TEXTBACKEND_REDIS_URL` 设置连接。使用 `-profile-cache-cluster` 对接 Redis Cluster，`-profile-cache-namespace` 用于隔离共享 Redis 的部署。启用 MySQL 配置时默认直接查询 MySQL。

## 契约与所有权

- `infra/redis` 是独立模块。每次 `New(Config)` 装配一个单机或 Cluster 客户端，接收对应的 Redis URL。解析配置后按命令连接。`Get` 区分缺失与成功读取的空字符串，`Set` 和 `SetIfAbsent` 通过原生命令设置值和期限，`Delete` 可重复调用。期限为零时创建持久值，正值使用毫秒或秒精度。调用方用 `Close` 释放资源。
- 命令继承调用上下文与期限。无效配置、无效 TTL、关闭和 Redis 命令失败使用包错误，取消和到期保留 Go 上下文语义。连接地址及凭据由装配入口提供。Redis Cluster 的槽位、重定向和连接池由 SDK 管理。
- 业务只向前缀源取值。缓存命中使用保存的前缀，未命中回源 MySQL，使用 `SET NX` 原子回填和设置过期时间。短时 Redis 失败沿用 MySQL 结果，并跳过本次回填。回源失败沿用 TextBackend 错误。缓存命令各有 50 毫秒上限，沿用父请求更短的期限。
- 同一命名空间和配置名共享缓存条目。MySQL 更新后的读取最多沿用缓存直到 TTL 到期。部署方可用不同命名空间区分数据库环境。进程排空调用后释放 Redis 连接和 MySQL 池。

## 复用与取舍

参考入口使用 Redis Cluster 查找业务字典，适配模块提供基础字符串、到期和原子占位方法。沿用 SDK 的单机与集群能力，公开字符串和布尔结果，调用方可区分空值与未命中。首个消费者用条件写入避免迟到的回填覆盖已经存在的缓存。应用拥有回源、键名和数据新鲜度。[Redis `SET`](https://redis.io/docs/latest/commands/set/) · [Redis Go 错误语义](https://redis.io/docs/latest/develop/clients/go/error-handling/)

采用 Go 1.24 起可用的 go-redis v9.22.0，兼容仓库 Go 1.25.0。go-redis v9.23.0 和 rueidis v1.0.80 的最低版本已提升到 Go 1.26。当前负载使用单键 GET、SET、DEL 与 TTL，直接使用现有命令和 Redis Cluster 解析。[go-redis v9.22.0 依赖](https://raw.githubusercontent.com/redis/go-redis/v9.22.0/go.mod) · [go-redis 发布说明](https://github.com/redis/go-redis/releases) · [rueidis v1.0.80 依赖](https://raw.githubusercontent.com/redis/rueidis/v1.0.80/go.mod)

## 通过条件

- 在 Ubuntu 连接官方 Redis 单机和三主节点 Cluster，对照原生 go-redis，验证空值、未命中、条件写入、TTL、删除、连接释放与上下文。保留原生 Go `Example`。
- 实际启动 TextBackend，观察从 MySQL 回填、缓存命中、TTL 到期后取得新前缀。让 Redis 响应失效后仍能取得 MySQL 结果，再完成真实进程关停。
- 验证 Go 模块边界、Linux 构建和运行，以及支持平台的构建证据。将装配条件与可观测行为对齐到使用文档。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 上设置 `KRONOS_REDIS_TEST_URL` 和 `KRONOS_REDIS_CLUSTER_TEST_URL`，执行 `GOWORK=off go -C infra/redis test -tags=redis_integration -count=3 -v ./...`。官方 Redis 8.4.8 单机及三主节点 Cluster 的对照各通过三轮，三个键归入三个不同主节点。确认空值、失效、`SET NX`、实际 TTL、取消与重复关闭。[官方 Redis 镜像](https://github.com/docker-library/official-images/blob/master/library/redis) · [Redis 对照与 Go 示例](../../infra/redis/integration_test.go)
- 在同一环境与 MySQL 8.4.13 运行 `GOWORK=off go test -tags='mysql_integration redis_integration' -count=3 -v ./test/integration/mysql`。真实 TextBackend 在单机和 Cluster 下写入初始缓存，MySQL 更新后仍读取旧缓存，过期后读取并缓存新值。Redis 接受 TCP 后保持不响应时，应用仍在一次 500 毫秒预算内返回更新后的 MySQL 值。各进程收到 `SIGTERM` 后正常退出。[进程和缓存回源测试](../../test/integration/mysql/cache_test.go)
- 复盘保留两种独立预算。Redis 读取失败即回源，省去一次无效回填。Go 竞态测试、分层检查、`go vet` 和 Linux 默认 Compose 回归通过，Redis 模块与 TextBackend 通过 Windows amd64 交叉构建。运行验收在 Linux 完成。[不响应期限测试](../../infra/redis/client_test.go)
