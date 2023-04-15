package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	sarama "github.com/Shopify/sarama"
	"github.com/netsampler/goflow2/transport"

	log "github.com/sirupsen/logrus"
)

type KafkaDriver struct {
	kafkaTLS            bool
	kafkaSASL           string
	kafkaSCRAM          string
	kafkaTopic          string
	kafkaSrv            string
	kafkaBrk            string
	kafkaMaxMsgBytes    int
	kafkaFlushBytes     int
	kafkaFlushFrequency time.Duration

	kafkaLogErrors bool

	kafkaHashing          bool
	kafkaVersion          string
	kafkaCompressionCodec string

	producer sarama.AsyncProducer

	q chan bool
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

	flag.BoolVar(&d.kafkaLogErrors, "transport.kafka.log.err", false, "Log Kafka errors")
	flag.BoolVar(&d.kafkaHashing, "transport.kafka.hashing", false, "Enable partition hashing")

	//flag.StringVar(&d.kafkaKeying, "transport.kafka.key", "SamplerAddress,DstAS", "Kafka list of fields to do hashing on (partition) separated by commas")
	flag.StringVar(&d.kafkaVersion, "transport.kafka.version", "2.8.0", "Kafka version")
	flag.StringVar(&d.kafkaCompressionCodec, "transport.kafka.compression", "", "Kafka default compression")

	return nil
}

func (d *KafkaDriver) Init() error {
	kafkaConfigVersion, err := sarama.ParseKafkaVersion(d.kafkaVersion)
	if err != nil {
		return err
	}

	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Version = kafkaConfigVersion
	kafkaConfig.Producer.Return.Successes = false
	kafkaConfig.Producer.Return.Errors = d.kafkaLogErrors
	kafkaConfig.Producer.MaxMessageBytes = d.kafkaMaxMsgBytes
	kafkaConfig.Producer.Flush.Bytes = d.kafkaFlushBytes
	kafkaConfig.Producer.Flush.Frequency = d.kafkaFlushFrequency

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
			return errors.New("compression codec does not exist")
		} else {
			kafkaConfig.Producer.Compression = cc
		}
	}

	if d.kafkaTLS {
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			return errors.New(fmt.Sprintf("Error initializing TLS: %v", err))
		}
		kafkaConfig.Net.TLS.Enable = true
		kafkaConfig.Net.TLS.Config = &tls.Config{RootCAs: rootCAs}
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
			return errors.New("Kafka SASL config from environment was unsuccessful. KAFKA_SASL_USER and KAFKA_SASL_PASS need to be set.")
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

	if d.kafkaLogErrors {
		go func() {
			for {
				select {
				case msg := <-kafkaProducer.Errors():
					//if log != nil {
					log.Error(msg)
					//}
				case <-d.q:
					return
				}
			}
		}()
	}

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
		return nil, errors.New(fmt.Sprintf("Service discovery: %v\n", err))
	}
	for _, srv := range srvs {
		addrs = append(addrs, net.JoinHostPort(srv.Target, strconv.Itoa(int(srv.Port))))
	}
	return addrs, nil
}

func init() {
	d := &KafkaDriver{}
	transport.RegisterTransportDriver("kafka", d)
}
