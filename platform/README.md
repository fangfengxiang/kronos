# 平台

承载多个消费者共用且需维护兼容性的约定、生成工具、参考模板和交付规范。接入约定随真实服务验证，服务自己的业务协议由该服务维护。

公开契约需说明发布、兼容和迁移责任。跨层装配由应用入口完成，平台约定经参考应用及容器 E2E 验证。[首版落地设计](../docs/design/first-vertical-slice.md)

[`platform/verify`](verify) 是独立 Go 命令模块，提供跨模块测试、分层检查和协议生成验收入口。[原生 Go 验收](../docs/design/go-verification.md)
