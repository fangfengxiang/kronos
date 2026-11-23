// Package kafka publishes acknowledged records and consumes Kafka groups.
package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

var (
	// ErrInvalidConfig indicates missing or invalid client settings.
	ErrInvalidConfig = errors.New("invalid Kafka configuration")
	// ErrUnavailable indicates that Kafka did not complete an operation.
	ErrUnavailable = errors.New("Kafka operation unavailable")
	// ErrClosed indicates that a client was already closed.
	ErrClosed = errors.New("Kafka client is closed")
)

var topicName = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,249}$`)

// PlainAuth configures SASL PLAIN. Set Config.TLS for an encrypted connection.
type PlainAuth struct {
	Username string
	Password string
}

// Config selects a preexisting topic and its Kafka connection settings.
type Config struct {
	Brokers []string
	Topic   string
	TLS     *tls.Config
	SASL    *PlainAuth
}

func clientOptions(config Config) ([]kgo.Opt, error) {
	if len(config.Brokers) == 0 || !topicName.MatchString(config.Topic) || config.Topic == "." || config.Topic == ".." {
		return nil, ErrInvalidConfig
	}
	for _, address := range config.Brokers {
		host, port, err := net.SplitHostPort(address)
		portNumber, portError := strconv.ParseUint(port, 10, 16)
		if err != nil || portError != nil || portNumber == 0 || host == "" || strings.ContainsAny(host, " /\t\r\n") {
			return nil, ErrInvalidConfig
		}
	}
	options := []kgo.Opt{kgo.SeedBrokers(slices.Clone(config.Brokers)...)}
	if config.TLS != nil {
		options = append(options, kgo.DialTLSConfig(config.TLS.Clone()))
	}
	if config.SASL != nil {
		if config.SASL.Username == "" || config.SASL.Password == "" {
			return nil, ErrInvalidConfig
		}
		mechanism := plain.Auth{User: config.SASL.Username, Pass: config.SASL.Password}.AsMechanism()
		options = append(options, kgo.SASL(mechanism))
	}
	return options, nil
}

func operationError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, kgo.ErrClientClosed):
		return ErrClosed
	default:
		return ErrUnavailable
	}
}
