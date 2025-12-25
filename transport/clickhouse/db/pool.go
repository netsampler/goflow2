package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

const (
	sqlClusterHosts      = "SELECT CONCAT(host_name, ':' ,toString(port)) as host FROM system.clusters WHERE cluster = ?"
	checkDeadInterval    = 10 * time.Minute
	revivingDeadInterval = 5 * time.Minute
)

type ConnPool struct {
	mux      sync.RWMutex
	pool     map[string]*connection
	deadPool map[string]*connection
}

type connection struct {
	diedAt  time.Time
	address string
	conn    ch.Conn
}

func NewConnPool(cluster string, options *ch.Options) (*ConnPool, error) {
	resp := ConnPool{
		pool:     make(map[string]*connection),
		deadPool: make(map[string]*connection),
	}

	conn, err := ch.Open(options)
	if err != nil {
		return nil, err
	}

	slog.Info("using configured servers to discover all cluster servers", slog.Any("servers", options.Addr))
	hosts, err := serversInCluster(cluster, conn)
	if err != nil {
		return nil, err
	}
	slog.Info("discovered servers", slog.Any("servers", hosts))
	addrInOptions := options.Addr
	for _, host := range hosts {
		options.Addr = []string{host}
		conn, err := ch.Open(options)
		if err != nil {
			slog.Error("failed to connect to server", host, err)
			continue
		}
		resp.pool[host] = &connection{
			conn:    conn,
			address: host,
		}
	}
	options.Addr = addrInOptions

	go resp.checkDead()
	return &resp, nil
}

func (c *ConnPool) RandomServerConnection(log *slog.Logger) (ch.Conn, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	pool := c.randomizedServersPool()
	for _, srvAddr := range pool {
		conn := c.pool[srvAddr]
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if conn.conn.Ping(pingCtx) == nil {
			cancel()
			log.Info("return connection", slog.String("host", srvAddr))
			return conn.conn, nil
		} else {
			log.Error("mark connection as dead", slog.String("host", srvAddr))
			c.markDead(srvAddr)
		}
		cancel()
	}
	return nil, fmt.Errorf("couldn't find live connection")
}

func (c *ConnPool) checkDead() {
	ticker := time.NewTicker(checkDeadInterval)
	defer ticker.Stop()
	for range ticker.C {
		c.mux.Lock()
		for srv, deadConn := range c.deadPool {
			slog.Info("reviving server", slog.String("host", srv))
			if time.Since(deadConn.diedAt) > revivingDeadInterval {
				delete(c.deadPool, srv)
				c.pool[srv] = deadConn
			}
		}
		c.mux.Unlock()
	}
}

func (c *ConnPool) markDead(address string) {
	conn := c.pool[address]
	conn.diedAt = time.Now()
	c.deadPool[address] = conn
	delete(c.pool, address)
}

func (c *ConnPool) randomizedServersPool() []string {
	keys := make([]string, 0, len(c.pool))
	for k := range c.pool {
		keys = append(keys, k)
	}
	rand.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	return keys
}

func serversInCluster(clusterName string, conn ch.Conn) ([]string, error) {
	type ClusterHost struct {
		HostName string `ch:"host"`
	}
	clusterHosts := []ClusterHost{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := conn.Select(ctx, &clusterHosts, sqlClusterHosts, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to select server names for cluster %s, %w", clusterName, err)
	}

	response := make([]string, 0, len(clusterHosts))
	for _, c := range clusterHosts {
		response = append(response, c.HostName)
	}
	if len(response) == 0 {
		return nil, errors.New("no hosts found in cluster")
	}
	return response, nil
}
