package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for IPAM
type Metrics struct {
	// IP allocation metrics
	IPAllocations     prometheus.Counter
	IPReleases        prometheus.Counter
	IPAllocationErrors prometheus.Counter

	// Block allocation metrics
	BlockAllocations prometheus.Counter
	BlockReleases    prometheus.Counter

	// Latency metrics
	AllocationDuration prometheus.Histogram
	ReleaseDuration    prometheus.Histogram

	// Pool metrics
	AvailableIPs *prometheus.GaugeVec
	UsedIPs      *prometheus.GaugeVec
	TotalIPs     *prometheus.GaugeVec

	// Block metrics
	BlocksPerNode *prometheus.GaugeVec
	BlockUsage    *prometheus.GaugeVec

	// Raft metrics
	RaftLeader    prometheus.Gauge
	RaftTerm      prometheus.Gauge
	RaftLastIndex prometheus.Gauge

	// Store metrics
	StoreMappings     prometheus.Gauge
	StoreOperations   *prometheus.CounterVec
	StoreOpDuration   *prometheus.HistogramVec
}

// NewMetrics creates a new Metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		// IP allocation counters
		IPAllocations: promauto.NewCounter(prometheus.CounterOpts{
			Name: "ipam_ip_allocations_total",
			Help: "Total number of IP allocations",
		}),
		IPReleases: promauto.NewCounter(prometheus.CounterOpts{
			Name: "ipam_ip_releases_total",
			Help: "Total number of IP releases",
		}),
		IPAllocationErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "ipam_ip_allocation_errors_total",
			Help: "Total number of IP allocation errors",
		}),

		// Block allocation counters
		BlockAllocations: promauto.NewCounter(prometheus.CounterOpts{
			Name: "ipam_block_allocations_total",
			Help: "Total number of block allocations",
		}),
		BlockReleases: promauto.NewCounter(prometheus.CounterOpts{
			Name: "ipam_block_releases_total",
			Help: "Total number of block releases",
		}),

		// Latency histograms
		AllocationDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "ipam_allocation_duration_seconds",
			Help:    "IP allocation duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		ReleaseDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "ipam_release_duration_seconds",
			Help:    "IP release duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),

		// Pool gauges
		AvailableIPs: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ipam_available_ips",
			Help: "Number of available IPs per node",
		}, []string{"node"}),
		UsedIPs: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ipam_used_ips",
			Help: "Number of used IPs per node",
		}, []string{"node"}),
		TotalIPs: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ipam_total_ips",
			Help: "Total number of IPs per node",
		}, []string{"node"}),

		// Block gauges
		BlocksPerNode: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ipam_blocks_per_node",
			Help: "Number of IP blocks allocated per node",
		}, []string{"node"}),
		BlockUsage: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ipam_block_usage_ratio",
			Help: "Block usage ratio (used/total) per node and block",
		}, []string{"node", "block_cidr"}),

		// Raft gauges
		RaftLeader: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "ipam_raft_leader",
			Help: "Whether this node is the Raft leader (1=leader, 0=follower)",
		}),
		RaftTerm: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "ipam_raft_term",
			Help: "Current Raft term",
		}),
		RaftLastIndex: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "ipam_raft_last_index",
			Help: "Last log index in Raft",
		}),

		// Store metrics
		StoreMappings: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "ipam_store_mappings_total",
			Help: "Total number of IP mappings in store",
		}),
		StoreOperations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "ipam_store_operations_total",
			Help: "Total number of store operations",
		}, []string{"operation", "status"}),
		StoreOpDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ipam_store_operation_duration_seconds",
			Help:    "Store operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
	}
}

// RecordIPAllocation records an IP allocation
func (m *Metrics) RecordIPAllocation(duration float64) {
	m.IPAllocations.Inc()
	m.AllocationDuration.Observe(duration)
}

// RecordIPRelease records an IP release
func (m *Metrics) RecordIPRelease(duration float64) {
	m.IPReleases.Inc()
	m.ReleaseDuration.Observe(duration)
}

// RecordIPAllocationError records an IP allocation error
func (m *Metrics) RecordIPAllocationError() {
	m.IPAllocationErrors.Inc()
}

// RecordBlockAllocation records a block allocation
func (m *Metrics) RecordBlockAllocation() {
	m.BlockAllocations.Inc()
}

// RecordBlockRelease records a block release
func (m *Metrics) RecordBlockRelease() {
	m.BlockReleases.Inc()
}

// UpdatePoolMetrics updates pool-related metrics
func (m *Metrics) UpdatePoolMetrics(nodeID string, available, used, total int) {
	m.AvailableIPs.WithLabelValues(nodeID).Set(float64(available))
	m.UsedIPs.WithLabelValues(nodeID).Set(float64(used))
	m.TotalIPs.WithLabelValues(nodeID).Set(float64(total))
}

// UpdateBlockMetrics updates block-related metrics
func (m *Metrics) UpdateBlockMetrics(nodeID string, blocks int) {
	m.BlocksPerNode.WithLabelValues(nodeID).Set(float64(blocks))
}

// UpdateBlockUsage updates block usage metrics
func (m *Metrics) UpdateBlockUsage(nodeID, blockCIDR string, ratio float64) {
	m.BlockUsage.WithLabelValues(nodeID, blockCIDR).Set(ratio)
}

// UpdateRaftMetrics updates Raft-related metrics
func (m *Metrics) UpdateRaftMetrics(isLeader bool, term, lastIndex uint64) {
	if isLeader {
		m.RaftLeader.Set(1)
	} else {
		m.RaftLeader.Set(0)
	}
	m.RaftTerm.Set(float64(term))
	m.RaftLastIndex.Set(float64(lastIndex))
}

// UpdateStoreMappings updates store mappings count
func (m *Metrics) UpdateStoreMappings(count int) {
	m.StoreMappings.Set(float64(count))
}

// RecordStoreOperation records a store operation
func (m *Metrics) RecordStoreOperation(operation, status string, duration float64) {
	m.StoreOperations.WithLabelValues(operation, status).Inc()
	m.StoreOpDuration.WithLabelValues(operation).Observe(duration)
}
