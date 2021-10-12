package pulsar

import (

	"context"
	"flag"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"

	"github.com/netsampler/goflow2/transport"

	log "github.com/sirupsen/logrus"
)

type PulsarDriver struct {

	pulsarServiceUrl string
	pulsarTopic string
	pulsarTimeouts int
	pulsarAsync bool

	client pulsar.Client
	producer pulsar.Producer

}

func (d *PulsarDriver) Prepare() error {

	flag.StringVar(&d.pulsarServiceUrl, "transport.pulsar.url", "pulsar://localhost:6650", "Pulsar Service URL")
	flag.StringVar(&d.pulsarTopic, "transport.pulsar.topic", "netflow", "Pulsar topic to produce to")
	flag.IntVar(&d.pulsarTimeouts, "transport.pulsar.timeouts", 30, "Pulsar timeouts (connection, operation)")
	flag.BoolVar(&d.pulsarAsync, "transport.pulsar.async", false, "Pulsar send mode async or sync")
	return nil
}

func (d *PulsarDriver) Init(context.Context) error {
	client, err := pulsar.NewClient(pulsar.ClientOptions {
		URL:               d.pulsarServiceUrl,
		OperationTimeout:  time.Duration(d.pulsarTimeouts) * time.Second,
		ConnectionTimeout: time.Duration(d.pulsarTimeouts) * time.Second,
	})
	if err != nil {
        log.Fatalf("Could not instantiate Pulsar client: %v", err)
		return err
    }
	d.client = client

	producer, err := client.CreateProducer(pulsar.ProducerOptions{
		Topic: d.pulsarTopic,
	})
	d.producer = producer

	if err != nil {
		log.Fatal(err)
	}

	return err
}

func (d *PulsarDriver) Send(key, data []byte) error {
	
	var err error;

	msg := pulsar.ProducerMessage{
		Payload: data,
	}
	ctx := context.Background()

	if (d.pulsarAsync) {
		d.producer.SendAsync(ctx, &msg, func(msgId pulsar.MessageID, msg *pulsar.ProducerMessage, err error) {
            if err != nil {
				log.Error("could not publish message id: %s - %s", msgId, err)
			}
			log.WithFields(log.Fields{
				"msgId": msgId,
			  }).Debug("msg published to pulsar")
		})
	} else {
		_, err = d.producer.Send(ctx, &msg)
	}
	
	return err
}

func (d *PulsarDriver) Close(context.Context) error {
	d.client.Close()
	return nil
}

func init() {
	d := &PulsarDriver{}
	transport.RegisterTransportDriver("pulsar", d)
}
