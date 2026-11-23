module github.com/fangfengxiang/kronos

go 1.25.0

require (
	github.com/fangfengxiang/kronos/core/locale v0.0.0
	github.com/fangfengxiang/kronos/framework/app v0.0.0
	github.com/fangfengxiang/kronos/framework/localization v0.0.0
	github.com/fangfengxiang/kronos/framework/log v0.0.0
	github.com/fangfengxiang/kronos/framework/request v0.0.0-20261123055539-816a8d9cbbb5
	github.com/fangfengxiang/kronos/framework/rpc v0.0.0
	github.com/fangfengxiang/kronos/infra/blades v0.0.0
	github.com/fangfengxiang/kronos/infra/configfile v0.0.0
	github.com/fangfengxiang/kronos/infra/microv2 v0.0.0
	github.com/fangfengxiang/kronos/infra/oteltrace v0.0.0
	github.com/fangfengxiang/kronos/infra/prometheus v0.0.0
	github.com/fangfengxiang/kronos/infra/runtimeprofile v0.0.0
	github.com/go-kratos/kratos/v3 v3.0.0
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/proto/otlp v1.10.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260511170946-3700d4141b60
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

replace (
	github.com/fangfengxiang/kronos/core/locale => ./core/locale
	github.com/fangfengxiang/kronos/framework/app => ./framework/app
	github.com/fangfengxiang/kronos/framework/localization => ./framework/localization
	github.com/fangfengxiang/kronos/framework/log => ./framework/log
	github.com/fangfengxiang/kronos/framework/request => ./framework/request
	github.com/fangfengxiang/kronos/framework/rpc => ./framework/rpc
	github.com/fangfengxiang/kronos/infra/blades => ./infra/blades
	github.com/fangfengxiang/kronos/infra/configfile => ./infra/configfile
	github.com/fangfengxiang/kronos/infra/microv2 => ./infra/microv2
	github.com/fangfengxiang/kronos/infra/oteltrace => ./infra/oteltrace
	github.com/fangfengxiang/kronos/infra/prometheus => ./infra/prometheus
	github.com/fangfengxiang/kronos/infra/runtimeprofile => ./infra/runtimeprofile
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/tableflip v1.2.3 // indirect
	github.com/go-kratos/blades v0.6.0 // indirect
	github.com/go-kratos/kratos/contrib/otel/v3 v3.0.0-20260626125723-668db92c2c00 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/form/v4 v4.3.0 // indirect
	github.com/google/jsonschema-go v0.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260511170946-3700d4141b60 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
