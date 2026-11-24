# 一元服务脚手架

## 使用者与目标

仓库中新服务的作者需要从 Proto 契约开始生成业务入口和类型化调用方。现有 Order 用例证明双协议接线可复用。脚手架把声明和执行入口固化为一个可以构建的最小示例，新业务从独立契约演进。[Order 统一装配](unary-service-api.md)

从仓库根目录运行 `go run ./platform/verify scaffold warehouse`。名称使用 1 到 32 个小写 ASCII 字母或数字，以字母开头，并遵守 Go 包名规则。工具只建立 `examples/warehouse`，其中包含独立的 Buf 配置、版本化 Proto、公共绑定、私有 Go 绑定、一个共享实现和两个命令。新服务的 Go 代码由根模块维护，沿用平台的 Go 依赖边界。

最小示例提供 `Ping` 方法。服务端通过 `kronos.New`、生成绑定、`Register` 和 `Run` 使用现有引擎。两协议接入同一实现。客户端以生成类型调用，默认 gRPC，显式可选 MicroRPC。启动入口配置 etcd，持有注册中心和客户端的关闭责任。

## 文件、失败与演进

- 暂存文件、Buf lint 和生成完成后，才取得目的目录。目的目录已存在时返回明确错误。创建失败回收本次临时文件和不完整输出。
- 目录内的 `buf.yaml` 和 `buf.gen.yaml` 随服务提交。`generate` 与 `proto` 发现 `examples/<名称>/buf.yaml` 后执行它的 lint、生成和产物检查。原有三个参考服务继续使用各自模板。
- `breaking` 检查已有服务的 Proto 快照。新增服务可以加入基线，后续同名服务按 Buf FILE 规则演进。移除已进入基线的服务需按版本迁移处理。[公开契约兼容](../adr/0010-public-contract-compatibility.md)
- 脚手架使用标准库和现有 Buf、Proto 插件。生成的业务和命令使用公开 Kronos 与协议类型。传输 SDK 限于生成的私有绑定。

### 由协议确定公开 Go 包

独立业务作者设置 Proto 的 `go_package` 时，已指定私有生成绑定所在的 `<公开 Go 包>/internal/wire[/子目录]`。原脚手架还在 Buf 的 Kronos 插件参数中复制同一个公开包路径。漏改会在迁移到独立模块后触发生成错误。

新模板省略 `api_import`。生成器从参加生成的文件的有效 Go 导入路径取得公开包，要求全部文件共享该公开包下的 `internal/wire`。混入另一公开包或缺少私有绑定根时，给出含 Proto 文件名的错误。只参加依赖解析的 Proto 不参与选择。Buf 的 `module` 参数继续控制生成文件的相对落盘位置。现有 `api_import` 保留显式选择及校验行为。[Protobuf Go 包选项](https://protobuf.dev/reference/go/go-generated/#package) · [Buf 生成配置](https://buf.build/docs/configuration/v2/buf-gen-yaml/)

采用现有 `protogen` 收到的描述符及 Go 导入路径。已有工程可保留显式参数，回退生成器时只需恢复这一项。通过条件为显式与推导产物逐字节相同，混合所有者被拒绝，独立项目可省掉一次包路径配置，命令与自动接线数量保持原有水平。使用真实 etcd 验证新模板生成、构建、双协议调用、正常关停及发布态依赖解析。

## 验收

### 仓库外业务项目

仓库外作者从空父目录开始时，需要一次生成属于自己的 Go 模块、Proto 和两个命令。继续使用同一 `verify` 程序和已有模板，提供 `scaffold-project <Go 模块路径> <Kronos 提交>`。模块路径的最后一段是服务包名和新目录名。调用者提供完整提交号，运行时、etcd 和生成器从公开 Go 代理按该提交解析。生成过程关闭 Go 工作区的依赖接管。在暂存目录完成模块初始化、依赖加载、Buf 校验、生成和依赖整理，最后才创建目标目录。目标已存在时返回错误，保留其文件。失败后可用同一参数重试。

旧路径需要从 Kronos 仓库生成、复制文件、重新填写四处路径或命令（业务导入、`go_package`、Buf `module`、生成器命令），并单独初始化模块、安装依赖与生成。新路径由一次命令根据业务模块路径和一个版本填写参数。调用者继续拥有生成后的业务源码、协议、配置及通过正式入口修改发布依赖的责任。需要回退时可继续使用下面的手工接入过程。

通过条件为发布态验收从没有 Go 工程的目录运行同一命令，核对无本地替换，重复运行保留已有业务文件，验证生成、构建、真实 etcd 发现、双协议业务返回和正常退出。

现有脚手架验收在临时目录复制 Kronos 根模块与相对替换。外部业务需要拥有自己的 Go 模块、Proto 包路径和产物。先验证源码接入路径。使用独立 Go 工作区接入固定版本的 Kronos 源码，将所用的 Kronos 模块作为工作区成员。Buf 通过模块导入路径运行同一生成器，业务的 `go.mod` 由业务维护。工作区路径由开发或构建环境管理。[Go 工作区](https://go.dev/ref/mod#workspaces)

仓库内继续使用原来的脚手架命令。验收项目共用同一 Proto 和 Go 接线模板，独立 Go 模块位于 Kronos 工作树之外，构建后由真实 etcd 完成默认 gRPC 与显式 MicroRPC 调用和关停。[外部项目操作](../guide/README.md#仓库外开发一元服务)

使用已推送提交时，业务模块解析 `platform/kronos` 和使用的注册中心，生成器按同一提交运行。上游 Go 只读取主模块的 `replace`，因此 `platform/kronos` 必须声明 Go 代理可获取的内部依赖。最小修复是把四个运行依赖固定到已经公开的 `076c829` 对应伪版本。现有本地替换继续供源码开发使用。有兼容性影响的依赖更新应先把依赖源码推送，再更新使用方版本。[Go 伪版本](https://go.dev/ref/mod#pseudo-versions) · [Go replace 范围](https://go.dev/ref/mod#go-mod-file-replace)

主分支推送后，以该次提交为版本，在临时业务目录关闭工作区并通过公开代理安装组件。核对业务模块没有替换，使用版本限定的生成器完成构建，随后跑通真实 etcd 的两条调用链。源码构建检查继续用于工作树和 PR。

在 Linux 使用锁定的 Go 和 Buf 从空的服务目录生成示例。核对目标冲突时保留原文件，新服务可通过相同的 lint、重新生成和依赖规则，客户端与服务端均可构建。用实际 etcd 运行生成服务，标准 gRPC 和显式 MicroRPC 都返回样例业务值。变更一个已发布字段的类型应触发兼容门禁。

## 实测

- 用当前 `verify` 源码从空父目录执行 `scaffold-project` 的测试。Go 1.25.0、Buf 1.70.0、Ubuntu 24.04 Linux arm64、真实 etcd v3.6.16，运行时、etcd 和生成器取自公开提交 `b078ddeb4f35ba4c0978bece2986e8a3fb307841`。独立 `go.mod` 无本地替换。构建后两协议返回业务值，服务正常退出。重跑保留已有业务源码，版本无法解析时清理暂存目录。[发布态回归](../../platform/verify/scaffold_test.go)

- Go 1.25.0、Buf 1.70.0、Ubuntu 24.04 Linux arm64 与 etcd v3.6.16 执行 `GOWORK=off go -C platform/verify test -count=1 -run '^Test(Scaffold|IndependentProject)BuildsFreshService$' -v ./...`。仓库内和独立项目均从省略 `api_import` 的模板生成、构建，发现并经双协议返回业务值，收到终止信号后以状态 0 退出。生成器竞态测试验证显式参数和推导产生同样的输出，两种错误归属被拒绝。全仓 `verify proto`、`verify breaking d51d1ac` 和 `verify deps` 通过。

- Go 1.25.0、Buf 1.70.0、Ubuntu 24.04 Linux arm64 与官方 etcd v3.6.16，运行 `GOWORK=off go -C platform/verify test -count=1 -run '^TestScaffoldBuildsFreshService$' -v ./...`。在隔离工作树中生成带数字名称的新服务，重生成的公开和私有绑定与原文件一致。两个实际命令通过 etcd 找到实例，分别返回 `仓库-收到回执`，服务随后正常退出。[生成、构建与调用](../../platform/verify/scaffold_test.go)
- macOS arm64 使用同一 Go 和 Buf 运行 `GOWORK=off go -C platform/verify test -race -count=2 -run '^Test(Scaffold|Breaking)' -v ./...`。两轮通过，验证目标文件保留，独立模块的字段变更及模块丢失能被兼容检查发现。真实工作树的协议生成和 `breaking be671e1` 通过。[独立模块兼容](../../platform/verify/compatibility_test.go)
- Go 1.25.0、Buf 1.70.0、Ubuntu 24.04 Linux arm64 与真实 etcd v3.6.16，运行 `GOWORK=off go -C platform/verify test -count=1 -run '^Test(Scaffold|IndependentProject)BuildsFreshService$' -v ./...`。两条路径均生成、构建、发现并经双协议返回指定业务值，服务进程收到终止信号后以状态 0 退出。macOS arm64 的工具竞态测试验证了两条路径的生成与构建。[独立项目测试](../../platform/verify/scaffold_test.go)
- 对 `076c829` 在外部模块执行 `go get` 得到四个 `v0.0.0` 依赖的 404。对修复后的 `9442fea` 设置 `KRONOS_PUBLISHED_REVISION`，在 Go 1.25.0、Buf 1.70.0、Ubuntu 24.04 Linux arm64 及真实 etcd v3.6.16 运行 `TestPublishedProjectBuildsFreshService`。公开 Go 代理成功解析运行时、注册中心和生成器，隔离业务模块生成、构建、经双协议返回预期值并正常关停。GitHub CI 的三个任务均通过，治理任务执行了发布态验收。[实际验收入口](../../platform/verify/scaffold_test.go) · [主线 CI](https://github.com/fangfengxiang/kronos/actions/runs/45380884576)
