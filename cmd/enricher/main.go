package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	flowmessage "github.com/netsampler/goflow2/v2/cmd/enricher/pb"

	// import various formatters
	"github.com/netsampler/goflow2/v2/format"
	_ "github.com/netsampler/goflow2/v2/format/binary"
	_ "github.com/netsampler/goflow2/v2/format/json"
	_ "github.com/netsampler/goflow2/v2/format/text"

	// import various transports
	"github.com/netsampler/goflow2/v2/transport"
	_ "github.com/netsampler/goflow2/v2/transport/file"
	_ "github.com/netsampler/goflow2/v2/transport/kafka"

	"github.com/oschwald/geoip2-golang"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protodelim"
)

var (
	version    = ""
	buildinfos = ""
	AppVersion = "Enricher " + version + " " + buildinfos

	DbAsn     = flag.String("db.asn", "", "IP->ASN database")
	DbCountry = flag.String("db.country", "", "IP->Country database")

	LogLevel = flag.String("loglevel", "info", "Log level")
	LogFmt   = flag.String("logfmt", "normal", "Log formatter")

	SamplingRate = flag.Int("samplingrate", 0, "Set sampling rate (values > 0)")

	Format    = flag.String("format", "json", fmt.Sprintf("Choose the format (available: %s)", strings.Join(format.GetFormats(), ", ")))
	Transport = flag.String("transport", "file", fmt.Sprintf("Choose the transport (available: %s)", strings.Join(transport.GetTransports(), ", ")))

	Version = flag.Bool("v", false, "Print version")
)

func MapAsn(db *geoip2.Reader, addr []byte, dest *uint32) {
	entry, err := db.ASN(net.IP(addr))
	if err != nil {
		return
	}
	*dest = uint32(entry.AutonomousSystemNumber)
}
func MapCountry(db *geoip2.Reader, addr []byte, dest *string) {
	entry, err := db.Country(net.IP(addr))
	if err != nil {
		return
	}
	*dest = entry.Country.IsoCode
}

func MapFlow(dbAsn, dbCountry *geoip2.Reader, msg *ProtoProducerMessage) {
	if dbAsn != nil {
		MapAsn(dbAsn, msg.SrcAddr, &(msg.FlowMessageExt.SrcAs))
		MapAsn(dbAsn, msg.DstAddr, &(msg.FlowMessageExt.DstAs))
	}
	if dbCountry != nil {
		MapCountry(dbCountry, msg.SrcAddr, &(msg.FlowMessageExt.SrcCountry))
		MapCountry(dbCountry, msg.DstAddr, &(msg.FlowMessageExt.DstCountry))
	}
}

type ProtoProducerMessage struct {
	flowmessage.FlowMessageExt
}

func (m *ProtoProducerMessage) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer([]byte{})
	_, err := protodelim.MarshalTo(buf, m)
	return buf.Bytes(), err
}

func main() {
	flag.Parse()

	if *Version {
		fmt.Println(AppVersion)
		os.Exit(0)
	}

	lvl, _ := log.ParseLevel(*LogLevel)
	log.SetLevel(lvl)

	var dbAsn, dbCountry *geoip2.Reader
	var err error
	if *DbAsn != "" {
		dbAsn, err = geoip2.Open(*DbAsn)
		if err != nil {
			log.Fatal(err)
		}
		defer dbAsn.Close()
	}

	if *DbCountry != "" {
		dbCountry, err = geoip2.Open(*DbCountry)
		if err != nil {
			log.Fatal(err)
		}
		defer dbCountry.Close()
	}

	formatter, err := format.FindFormat(*Format)
	if err != nil {
		log.Fatal(err)
	}

	transporter, err := transport.FindTransport(*Transport)
	if err != nil {
		log.Fatal(err)
	}
	defer transporter.Close()

	switch *LogFmt {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	}

	log.Info("starting enricher")

	rdr := bufio.NewReader(os.Stdin)

	var msg ProtoProducerMessage
	for {
		if err := protodelim.UnmarshalFrom(rdr, &msg); err != nil && errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			log.Error(err)
			continue
		}

		MapFlow(dbAsn, dbCountry, &msg)

		if *SamplingRate > 0 {
			msg.SamplingRate = uint64(*SamplingRate)
		}

		key, data, err := formatter.Format(&msg)
		if err != nil {
			log.Error(err)
			continue
		}

		err = transporter.Send(key, data)
		if err != nil {
			log.Error(err)
			continue
		}

		msg.Reset()
	}
}
