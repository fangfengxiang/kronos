// Package eventconfig assembles the optional Kafka connection for TextBackend commands.
package eventconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/fangfengxiang/kronos/infra/kafka"
)

// KafkaFlags holds the Kafka flags shared by the publisher and consumer commands.
type KafkaFlags struct {
	brokers    *string
	topic      *string
	tlsEnabled *bool
	caFile     *string
}

// RegisterKafkaFlags adds Kafka connection options to the command's flags.
func RegisterKafkaFlags(flags *flag.FlagSet) KafkaFlags {
	return KafkaFlags{
		brokers:    flags.String("kafka-brokers", "", "comma-separated Kafka broker addresses"),
		topic:      flags.String("kafka-topic", "", "Kafka topic for generated-text events"),
		tlsEnabled: flags.Bool("kafka-tls", false, "verify Kafka broker TLS using the system roots or specified CA"),
		caFile:     flags.String("kafka-ca-file", "", "Kafka broker CA certificate in PEM format (enables TLS)"),
	}
}

// Config resolves connection flags and environment credentials after parsing.
// With no brokers or topic, a zero Config represents a disabled publisher.
func (flags KafkaFlags) Config() (kafka.Config, error) {
	if *flags.brokers == "" && *flags.topic == "" && !*flags.tlsEnabled && *flags.caFile == "" {
		return kafka.Config{}, nil
	}
	if *flags.brokers == "" || *flags.topic == "" {
		return kafka.Config{}, errors.New("Kafka requires both kafka-brokers and kafka-topic")
	}
	brokers := strings.Split(*flags.brokers, ",")
	for index, broker := range brokers {
		brokers[index] = strings.TrimSpace(broker)
	}
	config := kafka.Config{Brokers: brokers, Topic: *flags.topic}
	username := os.Getenv("KRONOS_KAFKA_SASL_USERNAME")
	password := os.Getenv("KRONOS_KAFKA_SASL_PASSWORD")
	if username != "" || password != "" {
		config.SASL = &kafka.PlainAuth{Username: username, Password: password}
	}
	if *flags.tlsEnabled || *flags.caFile != "" {
		config.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if *flags.caFile != "" {
		certificate, err := os.ReadFile(*flags.caFile)
		if err != nil {
			return kafka.Config{}, fmt.Errorf("read Kafka CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certificate) {
			return kafka.Config{}, errors.New("Kafka CA file has no valid PEM certificate")
		}
		config.TLS.RootCAs = roots
	}
	return config, nil
}
