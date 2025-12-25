package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultChBatchSize                 = 200_000
	defaultChBatchInsertThreshold      = 0.7
	defaultChAttemptsBeforeForceInsert = 60
	defaultChInsertInterval            = 1 * time.Second
	defaultChMaxParallel               = 5
)

type DbConfig struct {
	Srv                       StringSliceFlag `env:"SRV"`
	Database                  string          `env:"DATABASE"`
	User                      string          `env:"USER"`
	Password                  Password        `env:"PASSWORD"`
	Cluster                   string          `env:"CLUSTER"`
	InsertInterval            time.Duration   `env:"INSERT_INTERVAL"`
	MaxParallelInserts        int             `env:"MAX_PARALLEL_INSERTS"`
	BatchInsertThreshold      float64         `env:"BATCH_INSERT_TH"`
	AttemptsBeforeForceInsert int             `env:"FORCE_INSERT_AFTER"`
	BatchSize                 int             `env:"BATCH_SIZE"`
}

func (t *DbConfig) Check() error {
	if len(t.Srv) == 0 {
		return errors.New("undefined srv")
	}
	if len(t.Database) == 0 {
		return errors.New("undefined database name")
	}
	if len(t.User) == 0 {
		return errors.New("undefined user")
	}
	if len(t.Password) == 0 {
		return errors.New("undefined password")
	}
	if len(t.Cluster) == 0 {
		return errors.New("undefined cluster ID")
	}
	if t.BatchSize <= 0 {
		return fmt.Errorf("batch size must be positive, got %d", t.BatchSize)
	}
	if t.BatchInsertThreshold < 0 || t.BatchInsertThreshold > 1 {
		return fmt.Errorf("batch insert threshold must be between 0 and 1, got %f", t.BatchInsertThreshold)
	}
	if t.MaxParallelInserts <= 0 {
		return fmt.Errorf("max parallel inserts must be positive, got %d", t.MaxParallelInserts)
	}
	if t.InsertInterval <= 0 {
		return fmt.Errorf("insert interval must be positive, got %v", t.InsertInterval)
	}
	return nil
}

func (t *DbConfig) SetDefaults() {
	t.InsertInterval = defaultChInsertInterval
	t.AttemptsBeforeForceInsert = defaultChAttemptsBeforeForceInsert
	t.MaxParallelInserts = defaultChMaxParallel
	t.BatchInsertThreshold = defaultChBatchInsertThreshold
	t.BatchSize = defaultChBatchSize
}
