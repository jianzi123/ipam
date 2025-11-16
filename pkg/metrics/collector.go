package metrics

import (
	"time"

	"github.com/jianzi123/ipam/pkg/ipam"
	"github.com/jianzi123/ipam/pkg/raft"
	"github.com/jianzi123/ipam/pkg/store"
)

// Collector periodically collects metrics from IPAM components
type Collector struct {
	metrics  *Metrics
	pool     *ipam.Pool
	raftNode *raft.Node
	store    *store.Store
	interval time.Duration
	stopCh   chan struct{}
}

// NewCollector creates a new metrics collector
func NewCollector(metrics *Metrics, pool *ipam.Pool, raftNode *raft.Node, store *store.Store, interval time.Duration) *Collector {
	return &Collector{
		metrics:  metrics,
		pool:     pool,
		raftNode: raftNode,
		store:    store,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start starts the metrics collection loop
func (c *Collector) Start() {
	ticker := time.NewTicker(c.interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				c.collect()
			case <-c.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops the metrics collection
func (c *Collector) Stop() {
	close(c.stopCh)
}

// collect collects all metrics
func (c *Collector) collect() {
	// Collect pool metrics
	c.collectPoolMetrics()

	// Collect Raft metrics
	c.collectRaftMetrics()

	// Collect store metrics
	c.collectStoreMetrics()
}

// collectPoolMetrics collects IP pool metrics
func (c *Collector) collectPoolMetrics() {
	stats := c.pool.GetStats()

	for nodeID, nodeStats := range stats.NodeStats {
		// Update per-node metrics
		c.metrics.UpdatePoolMetrics(
			nodeID,
			nodeStats.AvailableIPs,
			nodeStats.UsedIPs,
			nodeStats.TotalIPs,
		)

		// Update block count
		c.metrics.UpdateBlockMetrics(nodeID, nodeStats.Blocks)

		// Get blocks to update usage
		blocks, err := c.pool.GetNodeBlocks(nodeID)
		if err == nil {
			for _, block := range blocks {
				usage := block.Usage()
				c.metrics.UpdateBlockUsage(nodeID, block.CIDR.String(), usage)
			}
		}
	}
}

// collectRaftMetrics collects Raft metrics
func (c *Collector) collectRaftMetrics() {
	if c.raftNode == nil {
		return
	}

	isLeader := c.raftNode.IsLeader()

	// Get Raft stats
	stats := c.raftNode.Stats()

	// Parse term and last index from stats
	var term, lastIndex uint64
	if v, ok := stats["term"]; ok {
		// Parse term from string
		// In production, you'd use proper parsing
		_ = v
	}
	if v, ok := stats["last_log_index"]; ok {
		_ = v
	}

	c.metrics.UpdateRaftMetrics(isLeader, term, lastIndex)
}

// collectStoreMetrics collects store metrics
func (c *Collector) collectStoreMetrics() {
	if c.store == nil {
		return
	}

	stats, err := c.store.GetStats()
	if err == nil {
		c.metrics.UpdateStoreMappings(stats.TotalMappings)
	}
}
