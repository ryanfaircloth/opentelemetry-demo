// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/IBM/sarama"
	"github.com/xdg-go/scram"
)

var (
	Topic           = getTopic()
	ProtocolVersion = sarama.V3_0_0_0
)

func getTopic() string {
	if topic := os.Getenv("KAFKA_TOPIC"); topic != "" {
		return topic
	}
	return "orders"
}

// configureSecurity sets up SASL/TLS on saramaConfig from the KAFKA_PROTOCOL,
// KAFKA_SASL_MECHANISM, KAFKA_SASL_USERNAME, KAFKA_SASL_PASSWORD, and
// KAFKA_SSL_TRUSTSTORE_CRT env vars, matching the KafkaAccess-operator secret
// contract (https://github.com/strimzi/kafka-access-operator). Absent
// KAFKA_PROTOCOL (or PLAINTEXT), the plaintext/no-auth behavior is unchanged.
func configureSecurity(saramaConfig *sarama.Config) error {
	protocol := os.Getenv("KAFKA_PROTOCOL")
	if protocol == "" || protocol == "PLAINTEXT" {
		return nil
	}

	if strings.HasPrefix(protocol, "SASL_") {
		saramaConfig.Net.SASL.Enable = true
		saramaConfig.Net.SASL.User = os.Getenv("KAFKA_SASL_USERNAME")
		saramaConfig.Net.SASL.Password = os.Getenv("KAFKA_SASL_PASSWORD")
		saramaConfig.Net.SASL.Handshake = true

		switch os.Getenv("KAFKA_SASL_MECHANISM") {
		case "SCRAM-SHA-512":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: scram.SHA512}
			}
		case "SCRAM-SHA-256":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: scram.SHA256}
			}
		default:
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		}
	}

	if strings.HasSuffix(protocol, "SSL") {
		saramaConfig.Net.TLS.Enable = true
		tlsConfig := &tls.Config{}
		if caPEM := os.Getenv("KAFKA_SSL_TRUSTSTORE_CRT"); caPEM != "" {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(caPEM)) {
				return fmt.Errorf("failed to parse KAFKA_SSL_TRUSTSTORE_CRT as PEM")
			}
			tlsConfig.RootCAs = pool
		}
		saramaConfig.Net.TLS.Config = tlsConfig
	}

	return nil
}

type saramaLogger struct {
	logger *slog.Logger
}

func (l *saramaLogger) Printf(format string, v ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, v...))
}
func (l *saramaLogger) Println(v ...interface{}) {
	l.logger.Info(fmt.Sprint(v...))
}
func (l *saramaLogger) Print(v ...interface{}) {
	l.logger.Info(fmt.Sprint(v...))
}

func CreateKafkaProducer(brokers []string, logger *slog.Logger) (sarama.AsyncProducer, error) {
	// Set the logger for sarama to use.
	sarama.Logger = &saramaLogger{logger: logger}

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Return.Errors = true

	// Sarama has an issue in a single broker kafka if the kafka broker is restarted.
	// This setting is to prevent that issue from manifesting itself, but may swallow failed messages.
	saramaConfig.Producer.RequiredAcks = sarama.NoResponse

	saramaConfig.Version = ProtocolVersion

	// So we can know the partition and offset of messages.
	saramaConfig.Producer.Return.Successes = true

	if err := configureSecurity(saramaConfig); err != nil {
		return nil, err
	}

	producer, err := sarama.NewAsyncProducer(brokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	// We will log to STDOUT if we're not able to produce messages.
	go func() {
		for err := range producer.Errors() {
			logger.Error(fmt.Sprintf("Failed to write message: %+v", err))

		}
	}()
	return producer, nil
}
