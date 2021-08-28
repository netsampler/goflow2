package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/oschwald/geoip2-golang"

	"github.com/golang/protobuf/proto"
	flowmessage "github.com/netsampler/goflow2/cmd/enricher/pb"

	// import various formatters
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/format/common"
	_ "github.com/netsampler/goflow2/format/json"
	_ "github.com/netsampler/goflow2/format/protobuf"

	// import various transports
	"github.com/netsampler/goflow2/transport"
	_ "github.com/netsampler/goflow2/transport/file"
	_ "github.com/netsampler/goflow2/transport/kafka"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
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

	MetricsAddr = flag.String("metrics.addr", ":8081", "Metrics address")
	MetricsPath = flag.String("metrics.path", "/metrics", "Metrics path")

	TemplatePath = flag.String("templates.path", "/templates", "NetFlow/IPFIX templates list")

	Version = flag.Bool("v", false, "Print version")
)

func httpServer() {
	http.Handle(*MetricsPath, promhttp.Handler())
	log.Fatal(http.ListenAndServe(*MetricsAddr, nil))
}

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

func MapFlow(dbAsn, dbCountry *geoip2.Reader, msg *flowmessage.FlowMessageExt) {
	if dbAsn != nil {
		MapAsn(dbAsn, msg.SrcAddr, &(msg.SrcAS))
		MapAsn(dbAsn, msg.DstAddr, &(msg.DstAS))
	}
	if dbCountry != nil {
		MapCountry(dbCountry, msg.SrcAddr, &(msg.SrcCountry))
		MapCountry(dbCountry, msg.DstAddr, &(msg.DstCountry))
	}
}

func init() {
	common.AddTextField("SrcCountry", common.FORMAT_TYPE_STRING)
	common.AddTextField("DstCountry", common.FORMAT_TYPE_STRING)
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

	ctx := context.Background()

	formatter, err := format.FindFormat(ctx, *Format)
	if err != nil {
		log.Fatal(err)
	}

	transporter, err := transport.FindTransport(ctx, *Transport)
	if err != nil {
		log.Fatal(err)
	}
	defer transporter.Close(ctx)

	switch *LogFmt {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	}

	log.Info("Starting enricher")

	go httpServer()

	rdr := bufio.NewReader(os.Stdin)

	msg := &flowmessage.FlowMessageExt{}
	for {
		line, err := rdr.ReadBytes('\n')
		if err != nil && err != io.EOF {
			log.Error(err)
			continue
		}
		if len(line) == 0 {
			continue
		}
		line = bytes.TrimSuffix(line, []byte("\n"))

		err = proto.Unmarshal(line, msg)
		if err != nil {
			log.Error(err)
			continue
		}

		MapFlow(dbAsn, dbCountry, msg)

		if *SamplingRate > 0 {
			msg.SamplingRate = uint64(*SamplingRate)
		}

		key, data, err := formatter.Format(msg)
		if err != nil {
			log.Error(err)
			continue
		}

		err = transporter.Send(key, data)
		if err != nil {
			log.Error(err)
			continue
		}
	}
}
