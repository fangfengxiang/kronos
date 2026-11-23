//go:build kafka_integration

package kafka_test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/kafka"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestPublishAndGroupOffsetsMatchNativeKafka(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	brokers := testBrokers(t, "KRONOS_KAFKA_TEST_BROKERS")
	topic := createTopic(t, ctx, brokers)
	config := kafka.Config{Brokers: brokers, Topic: topic}
	startupContext, endStartup := context.WithCancel(ctx)
	publisher, err := kafka.OpenPublisher(startupContext, config)
	endStartup()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	const key = "kronos-request"
	const first = "已确认"
	const second = "需要重读"
	const third = "待处理"
	if err := publisher.Publish(ctx, []byte(key), []byte(first)); err != nil {
		t.Fatal(err)
	}
	native, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
		topic: {0: kgo.NewOffset().AtStart()},
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.Close)
	read := native.PollRecords(ctx, 1)
	if err := read.Err(); err != nil {
		t.Fatal(err)
	}
	records := read.Records()
	if len(records) != 1 || string(records[0].Key) != key || string(records[0].Value) != first || records[0].Offset != 0 {
		t.Fatalf("native received %v", records)
	}
	for _, value := range []string{second, third} {
		if err := native.ProduceSync(ctx, &kgo.Record{Topic: topic, Key: []byte(key), Value: []byte(value)}).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}
	group := "group-" + rand.Text()
	consumer, err := kafka.NewConsumer(config, group)
	if err != nil {
		t.Fatal(err)
	}
	stopBeforeCommit := errors.New("handler stopped before commit")
	for _, expected := range [][]string{{first, second}, {second, third}, {third}} {
		var received []string
		err := consumer.Run(ctx, func(_ context.Context, message kafka.Message) error {
			if string(message.Key) != key || message.Partition != 0 {
				return fmt.Errorf("unexpected key or partition at offset %d", message.Offset)
			}
			received = append(received, string(message.Value))
			if len(received) == len(expected) {
				return stopBeforeCommit
			}
			return nil
		})
		if !errors.Is(err, stopBeforeCommit) || strings.Join(received, "/") != strings.Join(expected, "/") {
			t.Fatalf("group received %v and %v, expected %v", received, err, expected)
		}
	}
	missing, err := kafka.OpenPublisher(ctx, kafka.Config{Brokers: brokers, Topic: "missing-" + rand.Text()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(missing.Close)
	publishContext, stopPublishing := context.WithTimeout(ctx, 25*time.Millisecond)
	defer stopPublishing()
	if err := missing.Publish(publishContext, []byte(key), []byte("cancel")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("publish to unavailable topic with a short deadline returned %v", err)
	}
	publisher.Close()
	if err := publisher.Publish(ctx, []byte(key), []byte(first)); !errors.Is(err, kafka.ErrClosed) {
		t.Fatalf("publish after Close returned %v", err)
	}
}

func TestKafkaWithTLSAndSASLPlain(t *testing.T) {
	if os.Getenv("KRONOS_KAFKA_TEST_TLS_BROKERS") == "" {
		t.Skip("set KRONOS_KAFKA_TEST_TLS_BROKERS for the TLS and SASL test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	topic := createTopic(t, ctx, testBrokers(t, "KRONOS_KAFKA_TEST_BROKERS"))
	pem, err := os.ReadFile(os.Getenv("KRONOS_KAFKA_TEST_CA_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		t.Fatal("test CA file carries no PEM certificate")
	}
	config := kafka.Config{
		Brokers: testBrokers(t, "KRONOS_KAFKA_TEST_TLS_BROKERS"), Topic: topic,
		TLS: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		SASL: &kafka.PlainAuth{
			Username: os.Getenv("KRONOS_KAFKA_TEST_SASL_USERNAME"), Password: os.Getenv("KRONOS_KAFKA_TEST_SASL_PASSWORD"),
		},
	}
	publisher, err := kafka.OpenPublisher(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	const payload = "authenticated"
	if err := publisher.Publish(ctx, nil, []byte(payload)); err != nil {
		t.Fatal(err)
	}
	consumer, err := kafka.NewConsumer(config, "secure-"+rand.Text())
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop after observed delivery")
	err = consumer.Run(ctx, func(_ context.Context, message kafka.Message) error {
		if string(message.Value) != payload || message.Key != nil {
			return errors.New("unexpected secured record")
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("secured consumer returned %v", err)
	}
	untrusted := config
	untrusted.TLS = &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}
	incorrectCredentials := config
	incorrectCredentials.SASL = &kafka.PlainAuth{Username: config.SASL.Username, Password: "wrong-fixture-credential"}
	for _, rejected := range []kafka.Config{untrusted, incorrectCredentials} {
		probe, cancelProbe := context.WithTimeout(ctx, 3*time.Second)
		client, err := kafka.OpenPublisher(probe, rejected)
		cancelProbe()
		if client != nil {
			client.Close()
		}
		if !errors.Is(err, kafka.ErrUnavailable) {
			t.Fatalf("Kafka accepted invalid peer or credentials: %v", err)
		}
	}
}

func testBrokers(t *testing.T, variable string) []string {
	t.Helper()
	addresses := os.Getenv(variable)
	if addresses == "" {
		t.Fatalf("set %s to an isolated Kafka broker", variable)
	}
	return strings.Split(addresses, ",")
}

func createTopic(t *testing.T, ctx context.Context, brokers []string) string {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	topic := "kronos-test-" + strings.ToLower(rand.Text())
	request := kmsg.NewPtrCreateTopicsRequest()
	request.Topics = []kmsg.CreateTopicsRequestTopic{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}}
	created, err := request.RequestWith(ctx, client)
	if err != nil || len(created.Topics) != 1 || created.Topics[0].ErrorCode != 0 {
		t.Fatalf("create isolated topic: %v, %v", created, err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		request := kmsg.NewPtrDeleteTopicsRequest()
		request.TopicNames = []string{topic}
		request.Topics = []kmsg.DeleteTopicsRequestTopic{{Topic: &topic}}
		_, _ = request.RequestWith(cleanup, client)
	})
	return topic
}
