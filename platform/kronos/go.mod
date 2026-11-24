module github.com/fangfengxiang/kronos/platform/kronos

go 1.25.0

require (
	github.com/fangfengxiang/kronos/framework/app v0.0.0-20261124105518-076c829db409
	github.com/fangfengxiang/kronos/framework/localization v0.0.0-20261124105518-076c829db409
	github.com/fangfengxiang/kronos/framework/request v0.0.0-20261123055539-816a8d9cbbb5
	github.com/fangfengxiang/kronos/framework/rpc v0.0.0-20261124105518-076c829db409
	github.com/fangfengxiang/kronos/infra/microv2 v0.0.0-20261124114118-a4b8d0515a8f
	github.com/go-kratos/kratos/v3 v3.0.0
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/cloudflare/tableflip v1.2.3 // indirect
	github.com/go-playground/form/v4 v4.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/kr/text v0.2.0 // indirect
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260511170946-3700d4141b60 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260511170946-3700d4141b60 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/fangfengxiang/kronos/framework/app => ../../framework/app
	github.com/fangfengxiang/kronos/framework/localization => ../../framework/localization
	github.com/fangfengxiang/kronos/framework/request => ../../framework/request
	github.com/fangfengxiang/kronos/framework/rpc => ../../framework/rpc
	github.com/fangfengxiang/kronos/infra/microv2 => ../../infra/microv2
)
