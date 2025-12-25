package config

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDbConfig_SetDefaults(t *testing.T) {
	t.Run("Sets all default values correctly", func(t *testing.T) {
		cfg := &DbConfig{}
		cfg.SetDefaults()

		assert.Equal(t, defaultChInsertInterval, cfg.InsertInterval)
		assert.Equal(t, defaultChAttemptsBeforeForceInsert, cfg.AttemptsBeforeForceInsert)
		assert.Equal(t, defaultChMaxParallel, cfg.MaxParallelInserts)
		assert.Equal(t, defaultChBatchInsertThreshold, cfg.BatchInsertThreshold)
		assert.Equal(t, defaultChBatchSize, cfg.BatchSize)
	})
}

func TestDbConfig_Check(t *testing.T) {
	t.Run("Valid config passes validation", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1", "server2"},
			Database: "testdb",
			User:     "testuser",
			Password: Password("testpass"),
			Cluster:  "testcluster",
		}
		cfg.SetDefaults()

		err := cfg.Check()
		assert.NoError(t, err)
	})

	t.Run("Missing srv returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Database: "testdb",
			User:     "testuser",
			Password: Password("testpass"),
			Cluster:  "testcluster",
		}

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined srv")
	})

	t.Run("Missing database returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1"},
			User:     "testuser",
			Password: Password("testpass"),
			Cluster:  "testcluster",
		}

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined database name")
	})

	t.Run("Missing user returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1"},
			Database: "testdb",
			Password: Password("testpass"),
			Cluster:  "testcluster",
		}

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined user")
	})

	t.Run("Missing password returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1"},
			Database: "testdb",
			User:     "testuser",
			Cluster:  "testcluster",
		}

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined password")
	})

	t.Run("Missing cluster returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1"},
			Database: "testdb",
			User:     "testuser",
			Password: Password("testpass"),
		}

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined cluster ID")
	})

	t.Run("Invalid batch size returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:       StringSliceFlag{"server1"},
			Database:  "testdb",
			User:      "testuser",
			Password:  Password("testpass"),
			Cluster:   "testcluster",
			BatchSize: -1,
		}
		cfg.SetDefaults()
		cfg.BatchSize = -1 // Override after defaults

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "batch size must be positive")
	})

	t.Run("Invalid batch insert threshold returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1"},
			Database: "testdb",
			User:     "testuser",
			Password: Password("testpass"),
			Cluster:  "testcluster",
		}
		cfg.SetDefaults()
		cfg.BatchInsertThreshold = 1.5 // Invalid value > 1

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "batch insert threshold must be between 0 and 1")
	})

	t.Run("Invalid max parallel inserts returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1"},
			Database: "testdb",
			User:     "testuser",
			Password: Password("testpass"),
			Cluster:  "testcluster",
		}
		cfg.SetDefaults()
		cfg.MaxParallelInserts = 0 // Invalid value

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max parallel inserts must be positive")
	})

	t.Run("Invalid insert interval returns error", func(t *testing.T) {
		cfg := &DbConfig{
			Srv:      StringSliceFlag{"server1"},
			Database: "testdb",
			User:     "testuser",
			Password: Password("testpass"),
			Cluster:  "testcluster",
		}
		cfg.SetDefaults()
		cfg.InsertInterval = -1 // Invalid value

		err := cfg.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insert interval must be positive")
	})
}

func TestStringSliceFlag(t *testing.T) {
	t.Run("String returns formatted slice", func(t *testing.T) {
		flag := StringSliceFlag{"item1", "item2", "item3"}
		result := flag.String()
		assert.Equal(t, "[item1 item2 item3]", result)
	})

	t.Run("Set appends single values", func(t *testing.T) {
		flag := StringSliceFlag{}

		err1 := flag.Set("item1")
		err2 := flag.Set("item2")
		err3 := flag.Set("item3")

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NoError(t, err3)
		assert.Equal(t, StringSliceFlag{"item1", "item2", "item3"}, flag)
	})

	t.Run("Set supports comma-separated values", func(t *testing.T) {
		flag := StringSliceFlag{}

		err := flag.Set("item1,item2,item3")

		assert.NoError(t, err)
		assert.Equal(t, StringSliceFlag{"item1", "item2", "item3"}, flag)
	})

	t.Run("Set handles comma-separated values with spaces", func(t *testing.T) {
		flag := StringSliceFlag{}

		err := flag.Set("item1, item2 , item3")

		assert.NoError(t, err)
		assert.Equal(t, StringSliceFlag{"item1", "item2", "item3"}, flag)
	})

	t.Run("Set ignores empty values in comma-separated list", func(t *testing.T) {
		flag := StringSliceFlag{}

		err := flag.Set("item1,,item2,")

		assert.NoError(t, err)
		assert.Equal(t, StringSliceFlag{"item1", "item2"}, flag)
	})

	t.Run("Set combines single and comma-separated values", func(t *testing.T) {
		flag := StringSliceFlag{}

		err1 := flag.Set("item1")
		err2 := flag.Set("item2,item3")
		err3 := flag.Set("item4")

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NoError(t, err3)
		assert.Equal(t, StringSliceFlag{"item1", "item2", "item3", "item4"}, flag)
	})

	t.Run("Empty flag returns empty slice string", func(t *testing.T) {
		flag := StringSliceFlag{}
		result := flag.String()
		assert.Equal(t, "[]", result)
	})
}

func TestPassword(t *testing.T) {
	t.Run("String returns masked password", func(t *testing.T) {
		password := Password("secret123")
		assert.Equal(t, "...", password.String())
	})

	t.Run("Expose returns actual password", func(t *testing.T) {
		password := Password("secret123")
		assert.Equal(t, "secret123", password.Expose())
	})

	t.Run("Set updates password value", func(t *testing.T) {
		password := Password("")
		err := password.Set("newsecret")

		assert.NoError(t, err)
		assert.Equal(t, "newsecret", password.Expose())
	})
}

func TestConfig_Prepare(t *testing.T) {
	t.Run("Prepare with default values", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		resolver, err := Prepare(fs)

		require.NoError(t, err)
		require.NotNil(t, resolver)

		dbConfig := resolver.Database()
		assert.Equal(t, defaultChInsertInterval, dbConfig.InsertInterval)
		assert.Equal(t, defaultChAttemptsBeforeForceInsert, dbConfig.AttemptsBeforeForceInsert)
		assert.Equal(t, defaultChMaxParallel, dbConfig.MaxParallelInserts)
		assert.Equal(t, defaultChBatchInsertThreshold, dbConfig.BatchInsertThreshold)
		assert.Equal(t, defaultChBatchSize, dbConfig.BatchSize)

		enrichers := resolver.Enrichers()
		assert.Empty(t, enrichers)
	})

	t.Run("Prepare with environment variables", func(t *testing.T) {
		envVars := map[string]string{
			"TRANSPORT_CLICKHOUSE_SRV":                  "server1,server2",
			"TRANSPORT_CLICKHOUSE_DATABASE":             "testdb",
			"TRANSPORT_CLICKHOUSE_USER":                 "testuser",
			"TRANSPORT_CLICKHOUSE_PASSWORD":             "testpass",
			"TRANSPORT_CLICKHOUSE_CLUSTER":              "testcluster",
			"TRANSPORT_CLICKHOUSE_ENRICHERS":            "enricher1,enricher2",
			"TRANSPORT_CLICKHOUSE_INSERT_INTERVAL":      "5s",
			"TRANSPORT_CLICKHOUSE_MAX_PARALLEL_INSERTS": "10",
			"TRANSPORT_CLICKHOUSE_BATCH_SIZE":           "1000",
			"TRANSPORT_CLICKHOUSE_BATCH_INSERT_TH":      "0.8",
			"TRANSPORT_CLICKHOUSE_FORCE_INSERT_AFTER":   "30",
		}

		for key, value := range envVars {
			t.Setenv(key, value)
		}

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		resolver, err := Prepare(fs)

		require.NoError(t, err)
		require.NotNil(t, resolver)

		dbConfig := resolver.Database()
		assert.Equal(t, StringSliceFlag{"server1", "server2"}, dbConfig.Srv)
		assert.Equal(t, "testdb", dbConfig.Database)
		assert.Equal(t, "testuser", dbConfig.User)
		assert.Equal(t, "testpass", dbConfig.Password.Expose())
		assert.Equal(t, "testcluster", dbConfig.Cluster)
		assert.Equal(t, 5*time.Second, dbConfig.InsertInterval)
		assert.Equal(t, 10, dbConfig.MaxParallelInserts)
		assert.Equal(t, 1000, dbConfig.BatchSize)
		assert.Equal(t, 0.8, dbConfig.BatchInsertThreshold)
		assert.Equal(t, 30, dbConfig.AttemptsBeforeForceInsert)

		enrichers := resolver.Enrichers()
		assert.Equal(t, []string{"enricher1", "enricher2"}, enrichers)
	})

	t.Run("Prepare registers flags correctly", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		_, err := Prepare(fs)

		require.NoError(t, err)

		// Check that flags are registered
		expectedFlags := []string{
			"transport.ch.servers",
			"transport.ch.cluster",
			"transport.ch.db",
			"transport.ch.user",
			"transport.ch.password",
			"transport.ch.insert.interval",
			"transport.ch.insert.parallel",
			"transport.ch.force.insert.after",
			"transport.ch.batch.size",
			"transport.ch.batch.insert.th",
			"transport.ch.enrichers",
		}

		for _, flagName := range expectedFlags {
			flag := fs.Lookup(flagName)
			assert.NotNil(t, flag, "Flag %s should be registered", flagName)
		}
	})

	t.Run("Prepare with command line flags", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		resolver, err := Prepare(fs)
		require.NoError(t, err)

		// Simulate command line arguments
		args := []string{
			"-transport.ch.servers", "cmdserver1",
			"-transport.ch.servers", "cmdserver2",
			"-transport.ch.db", "cmddb",
			"-transport.ch.user", "cmduser",
			"-transport.ch.password", "cmdpass",
			"-transport.ch.cluster", "cmdcluster",
			"-transport.ch.enrichers", "cmdenricher1",
			"-transport.ch.enrichers", "cmdenricher2",
			"-transport.ch.batch.size", "2000",
		}

		err = fs.Parse(args)
		require.NoError(t, err)

		dbConfig := resolver.Database()
		assert.Equal(t, "cmddb", dbConfig.Database)
		assert.Equal(t, "cmduser", dbConfig.User)
		assert.Equal(t, "cmdpass", dbConfig.Password.Expose())
		assert.Equal(t, "cmdcluster", dbConfig.Cluster)
		assert.Equal(t, 2000, dbConfig.BatchSize)
		assert.Equal(t, StringSliceFlag{"cmdserver1", "cmdserver2"}, dbConfig.Srv)

		enrichers := resolver.Enrichers()
		assert.Equal(t, []string{"cmdenricher1", "cmdenricher2"}, enrichers)
	})
}

func TestConfig_Interface(t *testing.T) {
	t.Run("Config implements ConfigResolver interface", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		resolver, err := Prepare(fs)

		require.NoError(t, err)

		var _ ConfigResolver = resolver

		dbConfig := resolver.Database()
		assert.NotNil(t, &dbConfig)

		enrichers := resolver.Enrichers()
		assert.NotNil(t, &enrichers)
	})
}

func TestConstants(t *testing.T) {
	t.Run("Constants have expected values", func(t *testing.T) {
		assert.Equal(t, "TRANSPORT_CLICKHOUSE_", envTransportChPrefix)
		assert.Equal(t, "transport.ch.", flagTransportChPrefix)
		assert.Equal(t, 200_000, defaultChBatchSize)
		assert.Equal(t, 0.7, defaultChBatchInsertThreshold)
		assert.Equal(t, 60, defaultChAttemptsBeforeForceInsert)
		assert.Equal(t, 1*time.Second, defaultChInsertInterval)
		assert.Equal(t, 5, defaultChMaxParallel)
	})
}

func TestConfig_ErrorHandling(t *testing.T) {
	t.Run("Invalid environment variable format", func(t *testing.T) {
		t.Setenv("TRANSPORT_CLICKHOUSE_INSERT_INTERVAL", "invalid-duration")

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		_, err := Prepare(fs)

		assert.Error(t, err)
	})
}
