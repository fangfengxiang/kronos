# Core 时区

## 消费者与最小契约

日报按时区取统计日期。调用方持有事件的绝对时刻和已选定的时区标识。应用配置或用户资料负责选择时区，语言、地区和进程的 `TZ` 各有独立用途。

| 接口 | 契约 |
| --- | --- |
| `timezone.Load(zoneID)` | 解析显式 IANA 时区标识，例如 `Asia/Shanghai`、`America/New_York` 或 `UTC`。返回标准库 `*time.Location`，供批处理或调度复用。时区数据源可提供别名。 |
| `timezone.Convert(instant, zoneID)` | 返回该时刻在目标时区的 `time.Time`。保留原时刻和纳秒精度，并按该时刻生效的时区规则计算当地日期和 UTC 偏移。 |
| `timezone.ErrInvalidZone` | 使用 `errors.Is` 判断时区缺失、标识无效或数据源无法加载。拒绝空值和主机本地时区别名。加载失败会保留 Go 标准库的错误链。 |

## 复用与部署

参考调用按配置加载时区，然后以当地日期查询当日记录。复用这条调用顺序。包边界显式返回加载错误，避免调用方把空位置交给 `Time.In`。固定偏移适用于约定固定偏移的输入。地区时区由 IANA 规则表达，时刻转换交给 Go 的 `time.LoadLocation` 和 `Time.In`。[Go time](https://pkg.go.dev/time#LoadLocation) · [IANA 时区数据库](https://www.iana.org/time-zones)

`core/timezone` 是独立 Go 模块，仅依赖标准库。Go 既能读取系统或部署方提供的时区数据，也能使用编译进应用的时区库。精简镜像用 `go build -tags timetzdata` 或在 `main` 导入 `time/tzdata`。应用负责更新系统数据或重新构建内嵌数据。[Go time/tzdata](https://pkg.go.dev/time/tzdata)

日期分组使用 `localTime.Format(time.DateOnly)`。预约场景从当地挂钟时间求时刻时，缺失与重复时刻的处理由该业务契约决定。

## 日报日期示例

日报归档命令接收一个 RFC3339 时刻和由调用方指定的 `-zone` IANA 时区。命令用 `timezone.Convert` 计算当地时刻并输出日期键。非法时刻和时区返回错误，调用方可以据此修正输入。[命令示例](../../examples/reportdate)

同一个时刻在洛杉矶归入上一日，在上海归入当日。示例用 Go `Example` 固定这两条输出。

```sh
GOWORK=off go run ./examples/reportdate -zone America/Los_Angeles 2025-01-02T01:00:00Z
GOWORK=off go run ./examples/reportdate -zone Asia/Shanghai 2025-01-02T01:00:00Z
```

## 通过条件

- 用公开接口将同一 UTC 时刻转换到两个地区，验证日期键跨日、时刻等值及纳秒精度。验证 UTC、非整小时偏移和无效时区错误。
- 遇到夏令时跳跃和回拨时，验证前后偏移。回拨期间两个绝对时刻可以显示为同一个当地时间，仍按各自偏移区分。
- 改变进程 `TZ` 后重跑包测试。另用一个无系统或 Go 安装时区文件访问权限的独立消费者，比较默认构建与 `timetzdata` 构建的结果。

## 实测与复盘

在 Go 1.25.0、macOS arm64 下，从模块目录分别以 `TZ=Pacific/Honolulu` 和 `TZ=Asia/Tokyo` 运行 `GOWORK=off go test -race -count=1 ./...`。两次结果一致。首次验收的 `go vet ./...`、`go mod tidy -diff`、全仓测试和分层检查均通过。[包测试与日报示例](../../core/timezone/timezone_test.go)

当前从仓库根目录运行 `go run ./platform/verify test` 和 `go run ./platform/verify deps`。[原生 Go 验收](go-verification.md)

独立 Go 消费者以 `GOOS=wasip1 GOARCH=wasm` 分别运行 `go build` 和 `go build -tags timetzdata`，在无预开放目录的 Node WASI 中执行。缺少时区数据时获得 `ErrInvalidZone`。内嵌构建将 `2025-01-02T01:00:00Z` 转为洛杉矶日报日期 `2025-01-01`。

用独立消费者在同一机器比较逐次解析时区名与预先加载位置，前者约 13 微秒每次，后者的 `Time.In` 路径无分配。这是单次主机微基准。批处理可在装配时调用 `Load`，逐条复用返回的位置。

Go 1.25.0、Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C core/timezone test -count=1 ./...` 和 `GOWORK=off go test -count=1 ./examples/reportdate` 均通过。运行两次日报日期示例，洛杉矶输出 `2025-01-01`，上海输出 `2025-01-02`。macOS arm64 的 `go run ./platform/verify test` 通过时区包和日报示例的竞态回归。[跨日示例](../../examples/reportdate/main_test.go)
