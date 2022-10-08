package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	sarama "github.com/Shopify/sarama"
	"github.com/netsampler/goflow2/transport"
	"github.com/netsampler/goflow2/utils"

	log "github.com/sirupsen/logrus"
)

type KafkaDriver struct {
	kafkaTLS            bool
	kafkaSASL           bool
	kafkaSCRAM          string
	kafkaTopic          string
	kafkaSrv            string
	kafkaBrk            string
	kafkaMaxMsgBytes    int
	kafkaFlushBytes     int
	kafkaFlushFrequency time.Duration

	kafkaLogErrors bool

	kafkaHashing bool
	kafkaVersion string
	kafkaCompressionCodec string

	producer sarama.AsyncProducer

	q chan bool
}

var (
	compressionCodecs = map[string]sarama.CompressionCodec{
		strings.ToLower(sarama.CompressionNone.String()): sarama.CompressionNone,
		strings.ToLower(sarama.CompressionGZIP.String()): sarama.CompressionGZIP,
		strings.ToLower(sarama.CompressionSnappy.String()): sarama.CompressionSnappy,
		strings.ToLower(sarama.CompressionLZ4.String()): sarama.CompressionLZ4,
		strings.ToLower(sarama.CompressionZSTD.String()): sarama.CompressionZSTD,
	}
)

func (d *KafkaDriver) Prepare() error {
	flag.BoolVar(&d.kafkaTLS, "transport.kafka.tls", false, "Use TLS to connect to Kafka")

	flag.BoolVar(&d.kafkaSASL, "transport.kafka.sasl", false, "Use SASL/PLAIN data to connect to Kafka (TLS is recommended and the environment variables KAFKA_SASL_USER and KAFKA_SASL_PASS need to be set)")
	flag.StringVar(&d.kafkaSCRAM, "transport.kafka.scram", "", "Use SASL/SCRAM with this SHA algorithm sha256 or sha512")

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

func (d *KafkaDriver) Init(context.Context) error {
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

	if d.kafkaSASL {
		if !d.kafkaTLS /*&& log != nil*/ {
			log.Warn("Using SASL without TLS will transmit the authentication in plaintext!")
		}
		kafkaConfig.Net.SASL.Enable = true
		kafkaConfig.Net.SASL.User = os.Getenv("KAFKA_SASL_USER")
		kafkaConfig.Net.SASL.Password = os.Getenv("KAFKA_SASL_PASS")
		if kafkaConfig.Net.SASL.User == "" && kafkaConfig.Net.SASL.Password == "" {
			return errors.New("Kafka SASL config from environment was unsuccessful. KAFKA_SASL_USER and KAFKA_SASL_PASS need to be set.")
		} else /*if log != nil*/ {
			log.Infof("Authenticating as user '%s'...", kafkaConfig.Net.SASL.User)
		}
	}

	if d.kafkaSCRAM != "" {
		if !d.kafkaSASL {
			return errors.New("option -transport.kafka.scram requires -transport.kafka.sasl")
		}
		kafkaConfig.Net.SASL.Handshake = true

		if d.kafkaSCRAM == "sha512" {
			kafkaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: SHA512}
			}
			kafkaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		} else if d.kafkaSCRAM == "sha256" {
			kafkaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: SHA256}
			}
			kafkaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256

		} else {
			log.Fatalf("invalid SHA algorithm \"%s\": can be either \"sha256\" or \"sha512\"", d.kafkaSCRAM)
		}
	}

	var addrs []string
	if d.kafkaSrv != "" {
		addrs, _ = utils.GetServiceAddresses(d.kafkaSrv)
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

func (d *KafkaDriver) Close(context.Context) error {
	d.producer.Close()
	close(d.q)
	return nil
}

func init() {
	d := &KafkaDriver{}
	transport.RegisterTransportDriver("kafka", d)
}
