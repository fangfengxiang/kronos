package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/examples/textbackend/internal/eventconfig"
	"github.com/fangfengxiang/kronos/framework/app"
	"github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/aliyunoss"
	"github.com/fangfengxiang/kronos/infra/consul"
	"github.com/fangfengxiang/kronos/infra/kafka"
	"github.com/fangfengxiang/kronos/infra/mongodb"
	"github.com/fangfengxiang/kronos/infra/redis"
)

func main() {
	logger := log.NewJSON(os.Stderr, nil).With("service", "text-backend")
	slog.SetDefault(logger)
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stopSignals()
	if err != nil {
		logger.ErrorContext(ctx, "text backend failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) (runError error) {
	flags := flag.NewFlagSet("textbackend", flag.ContinueOnError)
	grpcAddress := flags.String("grpc", "127.0.0.1:9290", "gRPC listen address")
	consulEndpoint := flags.String("consul", "", "Consul HTTP(S) agent URL (empty disables registration)")
	advertiseAddress := flags.String("advertise-grpc", "", "advertised gRPC host:port (defaults to the bound address)")
	mysqlProfileName := flags.String("mysql-profile", "", "text profile name in MySQL (empty uses the built-in profile)")
	mongoProfileName := flags.String("mongo-profile", "", "text profile ID in MongoDB")
	mongoDatabase := flags.String("mongo-database", "kronos", "MongoDB database for text profiles")
	ossProfileKey := flags.String("oss-profile", "", "text profile object key in OSS")
	ossUseCNAME := flags.Bool("oss-cname", false, "use an OSS bucket-bound domain for the configured endpoint")
	cacheTTL := flags.Duration("profile-cache-ttl", 0, "Redis TTL for MySQL profiles (zero disables the cache)")
	cacheNamespace := flags.String("profile-cache-namespace", "default", "namespace for MySQL profile keys in Redis")
	cacheCluster := flags.Bool("profile-cache-cluster", false, "use Redis Cluster for the profile cache")
	kafkaFlags := eventconfig.RegisterKafkaFlags(flags)
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	profileSourceCount := 0
	for _, selectedProfile := range []string{*mysqlProfileName, *mongoProfileName, *ossProfileKey} {
		if selectedProfile != "" {
			profileSourceCount++
		}
	}
	if profileSourceCount > 1 {
		return errors.New("configure only one of mysql-profile, mongo-profile, or oss-profile")
	}
	if *cacheTTL != 0 && (*cacheTTL < time.Millisecond || *mysqlProfileName == "" || strings.TrimSpace(*cacheNamespace) == "") {
		return errors.New("profile cache requires a MySQL profile, a namespace, and a TTL of at least one millisecond")
	}
	var registry *consul.Registry
	if *consulEndpoint != "" {
		var err error
		registry, err = consul.New(*consulEndpoint)
		if err != nil {
			return err
		}
		defer registry.Close()
	} else if *advertiseAddress != "" {
		return errors.New("advertise-grpc requires a Consul agent URL")
	}
	service := textService{}
	switch {
	case *mysqlProfileName != "":
		profile, err := openMySQLProfile(ctx, *mysqlProfileName, os.Getenv("KRONOS_TEXTBACKEND_MYSQL_DSN"))
		if err != nil {
			return err
		}
		defer func() { runError = errors.Join(runError, profile.pool.Close()) }()
		service.prefixSource = profile
	case *mongoProfileName != "":
		profile, err := openMongoProfile(ctx, mongodb.Config{
			URI: os.Getenv("KRONOS_TEXTBACKEND_MONGODB_URI"), Database: *mongoDatabase,
		}, *mongoProfileName)
		if err != nil {
			return err
		}
		defer func() { runError = errors.Join(runError, profile.close(ctx)) }()
		service.prefixSource = profile
	case *ossProfileKey != "":
		profile, err := openOSSProfile(ctx, aliyunoss.Config{
			Region: os.Getenv("KRONOS_TEXTBACKEND_OSS_REGION"), Bucket: os.Getenv("KRONOS_TEXTBACKEND_OSS_BUCKET"),
			Endpoint: os.Getenv("KRONOS_TEXTBACKEND_OSS_ENDPOINT"), UseCNAME: *ossUseCNAME,
			AccessKeyID: os.Getenv("OSS_ACCESS_KEY_ID"), AccessKeySecret: os.Getenv("OSS_ACCESS_KEY_SECRET"), SessionToken: os.Getenv("OSS_SESSION_TOKEN"),
		}, *ossProfileKey)
		if err != nil {
			return err
		}
		defer profile.bucket.CloseIdleConnections()
		service.prefixSource = profile
	}
	if *cacheTTL > 0 {
		cache, err := redis.New(redis.Config{URL: os.Getenv("KRONOS_TEXTBACKEND_REDIS_URL"), Cluster: *cacheCluster})
		if err != nil {
			return err
		}
		defer func() { runError = errors.Join(runError, cache.Close()) }()
		service.prefixSource = cachedProfile{
			origin: service.prefixSource, cache: cache, key: profileCacheKey(*cacheNamespace, *mysqlProfileName), ttl: *cacheTTL,
		}
	}
	eventConfig, err := kafkaFlags.Config()
	if err != nil {
		return err
	}
	if len(eventConfig.Brokers) != 0 {
		startupContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		publisher, err := kafka.OpenPublisher(startupContext, eventConfig)
		cancel()
		if err != nil {
			return err
		}
		defer publisher.Close()
		service.generationRecorder = kafkaGenerationRecorder{publisher: publisher}
	}
	application, err := app.New(app.Config{ShutdownTimeout: 5 * time.Second}, app.Binding{
		Name:    "gRPC",
		Address: *grpcAddress,
		Build: func(listener net.Listener) (app.Server, error) {
			server, err := textbackend.NewGRPCServer(listener, service)
			if err != nil || registry == nil {
				return server, err
			}
			address := *advertiseAddress
			if address == "" {
				address = listener.Addr().String()
			}
			return registerBackend(ctx, server, registry, address)
		},
	})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}

type prefixSource interface {
	Prefix(context.Context) (string, error)
}

type generationRecorder interface {
	RecordGeneration(context.Context, uint64) error
}

type textService struct {
	prefixSource       prefixSource
	generationRecorder generationRecorder
}

func (service textService) Generate(ctx context.Context, request *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prompt := request.GetPrompt()
	if strings.TrimSpace(prompt) == "" {
		return nil, &rpc.Error{
			Code:    rpc.InvalidArgument,
			Reason:  textbackend.ErrorReason_ERROR_REASON_INVALID_PROMPT.String(),
			Message: "prompt must not be blank",
		}
	}
	prefix := "backend: "
	if service.prefixSource != nil {
		var err error
		prefix, err = service.prefixSource.Prefix(ctx)
		if err != nil {
			return nil, err
		}
	}
	output := prefix + prompt
	if service.generationRecorder != nil {
		if err := service.generationRecorder.RecordGeneration(ctx, uint64(len(output))); err != nil {
			return nil, err
		}
	}
	return &textbackend.GenerateResponse{Text: output}, nil
}
