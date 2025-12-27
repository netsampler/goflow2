package config

import (
	"flag"

	"github.com/caarlos0/env/v11"
)

const (
	envTransportChPrefix  = "TRANSPORT_CLICKHOUSE_"
	flagTransportChPrefix = "transport.ch."
)

type ConfigResolver interface {
	Database() DbConfig
	Enrichers() []string
}

type config struct {
	DatabaseConfig  DbConfig
	EnrichersConfig StringSliceFlag `env:"ENRICHERS"`
}

// Prepare loads default values, parses environment variables, and registers command-line flags.
func Prepare(fs *flag.FlagSet) (ConfigResolver, error) {
	cfg := config{}
	cfg.DatabaseConfig.SetDefaults()
	if err := cfg.loadFromEnv(); err != nil {
		return nil, err
	}
	cfg.registerFlags(fs)
	return &cfg, nil
}

func (c *config) Database() DbConfig {
	return c.DatabaseConfig
}

func (c *config) Enrichers() []string {
	return c.EnrichersConfig
}

func (c *config) loadFromEnv() error {
	if err := env.ParseWithOptions(c, env.Options{
		Prefix: envTransportChPrefix,
	}); err != nil {
		return err
	}
	return nil
}

func (c *config) registerFlags(fs *flag.FlagSet) {
	fs.Var(&c.DatabaseConfig.Srv, flagTransportChPrefix+"servers", "Clickhouse servers, delimited by comma")
	fs.StringVar(&c.DatabaseConfig.Cluster, flagTransportChPrefix+"cluster", c.DatabaseConfig.Cluster, "Clickhouse cluster")
	fs.StringVar(&c.DatabaseConfig.Database, flagTransportChPrefix+"db", c.DatabaseConfig.Database, "db name")
	fs.StringVar(&c.DatabaseConfig.User, flagTransportChPrefix+"user", c.DatabaseConfig.User, "Database username")
	fs.Var(&c.DatabaseConfig.Password, flagTransportChPrefix+"password", "Database password")
	fs.DurationVar(&c.DatabaseConfig.InsertInterval, flagTransportChPrefix+"insert.interval", c.DatabaseConfig.InsertInterval, "interval before inserts")
	fs.IntVar(&c.DatabaseConfig.MaxParallelInserts, flagTransportChPrefix+"insert.parallel", c.DatabaseConfig.MaxParallelInserts, "maximum parallel inserts")
	fs.IntVar(&c.DatabaseConfig.AttemptsBeforeForceInsert, flagTransportChPrefix+"force.insert.after", c.DatabaseConfig.AttemptsBeforeForceInsert, "maximum insert iterations before ignore batch size")
	fs.IntVar(&c.DatabaseConfig.BatchSize, flagTransportChPrefix+"batch.size", c.DatabaseConfig.BatchSize, "batch size")
	fs.Float64Var(&c.DatabaseConfig.BatchInsertThreshold, flagTransportChPrefix+"batch.insert.th", c.DatabaseConfig.BatchInsertThreshold, "batch insert threshold")

	fs.Var(&c.EnrichersConfig, flagTransportChPrefix+"enrichers", "Enabled enrichers names, delimited by comma")
}
