module github.com/fangfengxiang/kronos

go 1.25.0

require (
	github.com/fangfengxiang/kronos/core/locale v0.0.0
	github.com/fangfengxiang/kronos/framework/app v0.0.0
	github.com/fangfengxiang/kronos/framework/localization v0.0.0
	github.com/fangfengxiang/kronos/framework/log v0.0.0
	github.com/fangfengxiang/kronos/framework/request v0.0.0-20261123055539-816a8d9cbbb5
	github.com/fangfengxiang/kronos/framework/rpc v0.0.0
	github.com/fangfengxiang/kronos/infra/aliyunoss v0.0.0
	github.com/fangfengxiang/kronos/infra/blades v0.0.0
	github.com/fangfengxiang/kronos/infra/configfile v0.0.0
	github.com/fangfengxiang/kronos/infra/consul v0.0.0
	github.com/fangfengxiang/kronos/infra/kafka v0.0.0
	github.com/fangfengxiang/kronos/infra/microv2 v0.0.0
	github.com/fangfengxiang/kronos/infra/mongodb v0.0.0
	github.com/fangfengxiang/kronos/infra/mysql v0.0.0
	github.com/fangfengxiang/kronos/infra/oteltrace v0.0.0
	github.com/fangfengxiang/kronos/infra/prometheus v0.0.0
	github.com/fangfengxiang/kronos/infra/redis v0.0.0
	github.com/fangfengxiang/kronos/infra/runtimeprofile v0.0.0
	github.com/go-kratos/kratos/v3 v3.0.0
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/proto/otlp v1.10.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260511170946-3700d4141b60
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260511170946-3700d4141b60
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
	github.com/fangfengxiang/kronos/infra/aliyunoss => ./infra/aliyunoss
	github.com/fangfengxiang/kronos/infra/blades => ./infra/blades
	github.com/fangfengxiang/kronos/infra/configfile => ./infra/configfile
	github.com/fangfengxiang/kronos/infra/consul => ./infra/consul
	github.com/fangfengxiang/kronos/infra/kafka => ./infra/kafka
	github.com/fangfengxiang/kronos/infra/microv2 => ./infra/microv2
	github.com/fangfengxiang/kronos/infra/mongodb => ./infra/mongodb
	github.com/fangfengxiang/kronos/infra/mysql => ./infra/mysql
	github.com/fangfengxiang/kronos/infra/oteltrace => ./infra/oteltrace
	github.com/fangfengxiang/kronos/infra/prometheus => ./infra/prometheus
	github.com/fangfengxiang/kronos/infra/redis => ./infra/redis
	github.com/fangfengxiang/kronos/infra/runtimeprofile => ./infra/runtimeprofile
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/aliyun/alibabacloud-oss-go-sdk-v2 v1.6.1 // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/tableflip v1.2.3 // indirect
	github.com/fatih/color v1.16.0 // indirect
	github.com/go-kratos/blades v0.6.0 // indirect
	github.com/go-kratos/kratos/contrib/otel/v3 v3.0.0-20260626125723-668db92c2c00 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/form/v4 v4.3.0 // indirect
	github.com/go-sql-driver/mysql v1.10.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/jsonschema-go v0.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hashicorp/consul/api v1.32.4 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.5.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/serf v0.10.1 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/redis/go-redis/v9 v9.22.0 // indirect
	github.com/twmb/franz-go v1.21.7 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	go.mongodb.org/mongo-driver/v2 v2.9.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/exp v0.0.0-20250808145144-a408d31f581a // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
