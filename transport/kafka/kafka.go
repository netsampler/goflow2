package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/netsampler/goflow2/v2/transport"

	sarama "github.com/Shopify/sarama"
)

type KafkaDriver struct {
	kafkaTLS         bool
	kafkaClientCert  string
	kafkaClientKey   string
	kafkaServerCA    string
	kafkaTlsInsecure bool

	kafkaSASL           string
	kafkaTopic          string
	kafkaSrv            string
	kafkaBrk            string
	kafkaMaxMsgBytes    int
	kafkaFlushBytes     int
	kafkaFlushFrequency time.Duration

	kafkaHashing          bool
	kafkaVersion          string
	kafkaCompressionCodec string

	producer sarama.AsyncProducer

	q chan bool

	errors chan error
}

// Error specifically for inner Kafka errors
type KafkaTransportError struct {
	Err error
}

func (e *KafkaTransportError) Error() string {
	return fmt.Sprintf("kafka transport %s", e.Err.Error())
}
func (e *KafkaTransportError) Unwrap() []error {
	return []error{transport.ErrTransport, e.Err}
}

type KafkaSASLAlgorithm string

const (
	KAFKA_SASL_NONE         KafkaSASLAlgorithm = "none"
	KAFKA_SASL_PLAIN        KafkaSASLAlgorithm = "plain"
	KAFKA_SASL_SCRAM_SHA256 KafkaSASLAlgorithm = "scram-sha256"
	KAFKA_SASL_SCRAM_SHA512 KafkaSASLAlgorithm = "scram-sha512"
)

var (
	compressionCodecs = map[string]sarama.CompressionCodec{
		strings.ToLower(sarama.CompressionNone.String()):   sarama.CompressionNone,
		strings.ToLower(sarama.CompressionGZIP.String()):   sarama.CompressionGZIP,
		strings.ToLower(sarama.CompressionSnappy.String()): sarama.CompressionSnappy,
		strings.ToLower(sarama.CompressionLZ4.String()):    sarama.CompressionLZ4,
		strings.ToLower(sarama.CompressionZSTD.String()):   sarama.CompressionZSTD,
	}

	saslAlgorithms = map[KafkaSASLAlgorithm]bool{
		KAFKA_SASL_PLAIN:        true,
		KAFKA_SASL_SCRAM_SHA256: true,
		KAFKA_SASL_SCRAM_SHA512: true,
	}
	saslAlgorithmsList = []string{
		string(KAFKA_SASL_NONE),
		string(KAFKA_SASL_PLAIN),
		string(KAFKA_SASL_SCRAM_SHA256),
		string(KAFKA_SASL_SCRAM_SHA512),
	}
)

func (d *KafkaDriver) Prepare() error {
	flag.BoolVar(&d.kafkaTLS, "transport.kafka.tls", false, "Use TLS to connect to Kafka")

	flag.StringVar(&d.kafkaClientCert, "transport.kafka.tls.client", "", "Kafka client certificate")
	flag.StringVar(&d.kafkaClientKey, "transport.kafka.tls.key", "", "Kafka client key")
	flag.StringVar(&d.kafkaServerCA, "transport.kafka.tls.ca", "", "Kafka certificate authority")
	flag.BoolVar(&d.kafkaTlsInsecure, "transport.kafka.tls.insecure", false, "Skips TLS verification")

	flag.StringVar(&d.kafkaSASL, "transport.kafka.sasl", "none",
		fmt.Sprintf(
			"Use SASL to connect to Kafka, available settings: %s (TLS is recommended and the environment variables KAFKA_SASL_USER and KAFKA_SASL_PASS need to be set)",
			strings.Join(saslAlgorithmsList, ", ")))

	flag.StringVar(&d.kafkaTopic, "transport.kafka.topic", "flow-messages", "Kafka topic to produce to")
	flag.StringVar(&d.kafkaSrv, "transport.kafka.srv", "", "SRV record containing a list of Kafka brokers (or use brokers)")
	flag.StringVar(&d.kafkaBrk, "transport.kafka.brokers", "127.0.0.1:9092,[::1]:9092", "Kafka brokers list separated by commas")
	flag.IntVar(&d.kafkaMaxMsgBytes, "transport.kafka.maxmsgbytes", 1000000, "Kafka max message bytes")
	flag.IntVar(&d.kafkaFlushBytes, "transport.kafka.flushbytes", int(sarama.MaxRequestSize), "Kafka flush bytes")
	flag.DurationVar(&d.kafkaFlushFrequency, "transport.kafka.flushfreq", time.Second*5, "Kafka flush frequency")

	flag.BoolVar(&d.kafkaHashing, "transport.kafka.hashing", false, "Enable partition hashing")

	flag.StringVar(&d.kafkaVersion, "transport.kafka.version", "2.8.0", "Kafka version")
	flag.StringVar(&d.kafkaCompressionCodec, "transport.kafka.compression", "", "Kafka default compression")

	return nil
}

func (d *KafkaDriver) Errors() <-chan error {
	return d.errors
}

func (d *KafkaDriver) Init() error {
	kafkaConfigVersion, err := sarama.ParseKafkaVersion(d.kafkaVersion)
	if err != nil {
		return err
	}

	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Version = kafkaConfigVersion
	kafkaConfig.Producer.Return.Successes = false
	kafkaConfig.Producer.Return.Errors = true
	kafkaConfig.Producer.MaxMessageBytes = d.kafkaMaxMsgBytes
	kafkaConfig.Producer.Flush.Bytes = d.kafkaFlushBytes
	kafkaConfig.Producer.Flush.Frequency = d.kafkaFlushFrequency
	kafkaConfig.Producer.Partitioner = sarama.NewRoundRobinPartitioner

	if d.kafkaCompressionCodec != "" {
		/*
			// when upgrading sarama, replace with:
			// note: if the library adds more codecs, they will be supported natively
			var cc *sarama.CompressionCodec

			if err := cc.UnmarshalText([]byte(d.kafkaCompressionCodec)); err != nil {
				return err
			}
			kafkaConfig.Producer.Compression = *cc
		*/

		if cc, ok := compressionCodecs[strings.ToLower(d.kafkaCompressionCodec)]; !ok {
			return fmt.Errorf("compression codec does not exist")
		} else {
			kafkaConfig.Producer.Compression = cc
		}
	}

	if d.kafkaTLS {
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			return fmt.Errorf("error initializing TLS: %v", err)
		}
		kafkaConfig.Net.TLS.Enable = true
		kafkaConfig.Net.TLS.Config = &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
		}

		kafkaConfig.Net.TLS.Config.InsecureSkipVerify = d.kafkaTlsInsecure

		if d.kafkaServerCA != "" {
			serverCaFile, err := os.Open(d.kafkaServerCA)
			if err != nil {
				return fmt.Errorf("error initializing server CA: %v", err)
			}

			serverCaBytes, err := io.ReadAll(serverCaFile)
			serverCaFile.Close()
			if err != nil {
				return fmt.Errorf("error reading server CA: %v", err)
			}

			block, _ := pem.Decode(serverCaBytes)

			serverCa, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return fmt.Errorf("error parsing server CA: %v", err)
			}

			certPool := x509.NewCertPool()
			certPool.AddCert(serverCa)

			kafkaConfig.Net.TLS.Config.RootCAs = certPool
		}

		if d.kafkaClientCert != "" && d.kafkaClientKey != "" {
			_, err := tls.LoadX509KeyPair(d.kafkaClientCert, d.kafkaClientKey)
			if err != nil {
				return fmt.Errorf("error initializing mTLS: %v", err)
			}

			kafkaConfig.Net.TLS.Config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				cert, err := tls.LoadX509KeyPair(d.kafkaClientCert, d.kafkaClientKey)
				if err != nil {
					return nil, err
				}
				return &cert, nil
			}
		}

	}

	if d.kafkaHashing {
		kafkaConfig.Producer.Partitioner = sarama.NewHashPartitioner
	}

	kafkaSASL := KafkaSASLAlgorithm(d.kafkaSASL)
	if d.kafkaSASL != "" && kafkaSASL != KAFKA_SASL_NONE {
		_, ok := saslAlgorithms[KafkaSASLAlgorithm(strings.ToLower(d.kafkaSASL))]
		if !ok {
			return errors.New("SASL algorithm does not exist")
		}

		kafkaConfig.Net.SASL.Enable = true
		kafkaConfig.Net.SASL.User = os.Getenv("KAFKA_SASL_USER")
		kafkaConfig.Net.SASL.Password = os.Getenv("KAFKA_SASL_PASS")
		if kafkaConfig.Net.SASL.User == "" && kafkaConfig.Net.SASL.Password == "" {
			return fmt.Errorf("Kafka SASL config from environment was unsuccessful. KAFKA_SASL_USER and KAFKA_SASL_PASS need to be set.")
		}

		if kafkaSASL == KAFKA_SASL_SCRAM_SHA256 || kafkaSASL == KAFKA_SASL_SCRAM_SHA512 {
			kafkaConfig.Net.SASL.Handshake = true

			if kafkaSASL == KAFKA_SASL_SCRAM_SHA512 {
				kafkaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
					return &XDGSCRAMClient{HashGeneratorFcn: SHA512}
				}
				kafkaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			} else if kafkaSASL == KAFKA_SASL_SCRAM_SHA256 {
				kafkaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
					return &XDGSCRAMClient{HashGeneratorFcn: SHA256}
				}
				kafkaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			}
		}
	}

	var addrs []string
	if d.kafkaSrv != "" {
		addrs, _ = GetServiceAddresses(d.kafkaSrv)
	} else {
		addrs = strings.Split(d.kafkaBrk, ",")
	}

	kafkaProducer, err := sarama.NewAsyncProducer(addrs, kafkaConfig)
	if err != nil {
		return err
	}
	d.producer = kafkaProducer

	d.q = make(chan bool)

	go func() {
		for {
			select {
			case msg := <-kafkaProducer.Errors():
				var err error
				if msg != nil {
					err = &KafkaTransportError{msg}
				}
				select {
				case d.errors <- err:
				default:
				}

				if msg == nil {
					return
				}
			case <-d.q:
				return
			}
		}
	}()

	return err
}

func (d *KafkaDriver) Send(key, data []byte) error {
	d.producer.Input() <- &sarama.ProducerMessage{
		Topic: d.kafkaTopic,
		Key:   sarama.ByteEncoder(key),
		Value: sarama.ByteEncoder(data),
	}
	return nil
}

func (d *KafkaDriver) Close() error {
	d.producer.Close()
	close(d.q)
	return nil
}

// todo: deprecate?
func GetServiceAddresses(srv string) (addrs []string, err error) {
	_, srvs, err := net.LookupSRV("", "", srv)
	if err != nil {
		return nil, fmt.Errorf("service discovery: %v\n", err)
	}
	for _, srv := range srvs {
		addrs = append(addrs, net.JoinHostPort(srv.Target, strconv.Itoa(int(srv.Port))))
	}
	return addrs, nil
}

func init() {
	d := &KafkaDriver{
		errors: make(chan error),
	}
	transport.RegisterTransportDriver("kafka", d)
}
