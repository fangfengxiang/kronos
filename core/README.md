# 核心原语

承载有明确通用语义的值、算法和并发原语。适用性由实际消费者和包级测试验证。本层保持独立于进程装配与外部 SDK。

组件按首版参考应用的消费场景落地。范围、取舍和验收证据见[Core 首版落地设计](../docs/design/core-v1.md)。

## 组件

- `runtime.Go` 启动 goroutine 并报告终态。调用方按需注入恢复后处理、观察和日志。[独立模块](runtime/go.mod) · [Runtime Go 设计](../docs/design/runtime-go.md)
- `locale.Matcher` 按用户偏好选择已登记语言，`locale.Catalog` 按消息键查找和回退，返回实际文案语言。资源由上层维持。[独立模块](locale/go.mod) · [语言匹配与消息资源](../docs/design/core-localization.md)
- `timezone.Load` 读取显式 IANA 时区，`timezone.Convert` 把绝对时刻转换为当地时间。精简部署可在应用构建时内嵌时区数据。[独立模块](timezone/go.mod) · [时区设计](../docs/design/core-timezone.md)
