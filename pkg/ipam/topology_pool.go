package ipam

import (
	"fmt"
	"net"
	"sync"

	"github.com/jianzi123/ipam/pkg/topology"
)

// TopologyAwarePool 是拓扑感知的 IP 池
// 它基于网络拓扑（Zone/Pod/TOR）进行 IP 分配
type TopologyAwarePool struct {
	clusterCIDR string
	topology    *topology.Topology
	subnetPools map[string]*topology.SubnetPool // tor_id -> SubnetPool

	mu sync.RWMutex
}

// NewTopologyAwarePool 创建一个新的拓扑感知 IP 池
func NewTopologyAwarePool(clusterCIDR string) *TopologyAwarePool {
	return &TopologyAwarePool{
		clusterCIDR: clusterCIDR,
		topology:    topology.NewTopology(),
		subnetPools: make(map[string]*topology.SubnetPool),
	}
}

// InitializeTopology 初始化网络拓扑
func (p *TopologyAwarePool) InitializeTopology(topoConfig *TopologyConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 添加 Zone
	for _, zoneConfig := range topoConfig.Zones {
		zone := &topology.Zone{
			ID:           zoneConfig.ID,
			Name:         zoneConfig.Name,
			SubnetRanges: zoneConfig.SubnetRanges,
		}
		if err := p.topology.AddZone(zone); err != nil {
			return fmt.Errorf("failed to add zone %s: %w", zone.ID, err)
		}

		// 添加 Pod
		for _, podConfig := range zoneConfig.Pods {
			pod := &topology.Pod{
				ID:           podConfig.ID,
				Name:         podConfig.Name,
				ZoneID:       zone.ID,
				SubnetRanges: podConfig.SubnetRanges,
			}
			if err := p.topology.AddPod(pod); err != nil {
				return fmt.Errorf("failed to add pod %s: %w", pod.ID, err)
			}

			// 添加 TOR
			for _, torConfig := range podConfig.TORs {
				tor := &topology.TOR{
					ID:       torConfig.ID,
					Name:     torConfig.Name,
					PodID:    pod.ID,
					Location: torConfig.Location,
					Subnets:  make([]string, 0),
				}
				if err := p.topology.AddTOR(tor); err != nil {
					return fmt.Errorf("failed to add TOR %s: %w", tor.ID, err)
				}

				// 为 TOR 创建网段池
				subnetPool := topology.NewSubnetPool(tor.ID)
				for _, subnetConfig := range torConfig.Subnets {
					if err := subnetPool.AddSubnet(subnetConfig.CIDR, subnetConfig.Purpose); err != nil {
						return fmt.Errorf("failed to add subnet %s to TOR %s: %w", subnetConfig.CIDR, tor.ID, err)
					}
					// 同时添加到 TOR 的网段列表
					p.topology.AddSubnetToTOR(tor.ID, subnetConfig.CIDR)
				}
				p.subnetPools[tor.ID] = subnetPool
			}
		}
	}

	return nil
}

// RegisterNode 注册一个节点
func (p *TopologyAwarePool) RegisterNode(nodeID, nodeName, torID string, labels map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	node := &topology.Node{
		ID:     nodeID,
		Name:   nodeName,
		TORID:  torID,
		Labels: labels,
	}

	return p.topology.RegisterNode(node)
}

// AllocateIPForNode 为节点上的容器分配 IP
// 基于拓扑感知：从节点所属 TOR 的网段池中分配
func (p *TopologyAwarePool) AllocateIPForNode(nodeID string) (net.IP, string, error) {
	return p.AllocateIPForNodeWithPurpose(nodeID, "default")
}

// AllocateIPForNodeWithPurpose 为节点分配指定用途的 IP
func (p *TopologyAwarePool) AllocateIPForNodeWithPurpose(nodeID, purpose string) (net.IP, string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 1. 查找节点所属的 TOR
	tor, err := p.topology.GetNodeTOR(nodeID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get TOR for node %s: %w", nodeID, err)
	}

	// 2. 获取 TOR 的网段池
	subnetPool, exists := p.subnetPools[tor.ID]
	if !exists {
		return nil, "", fmt.Errorf("subnet pool not found for TOR %s", tor.ID)
	}

	// 3. 从网段池分配 IP
	ip, cidr, err := subnetPool.AllocateIP(nodeID, purpose)
	if err != nil {
		return nil, "", fmt.Errorf("failed to allocate IP from TOR %s: %w", tor.ID, err)
	}

	return ip, cidr, nil
}

// ReleaseIPForNode 释放节点的 IP
func (p *TopologyAwarePool) ReleaseIPForNode(nodeID string, ip net.IP) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 查找节点所属的 TOR
	tor, err := p.topology.GetNodeTOR(nodeID)
	if err != nil {
		return fmt.Errorf("failed to get TOR for node %s: %w", nodeID, err)
	}

	// 获取 TOR 的网段池
	subnetPool, exists := p.subnetPools[tor.ID]
	if !exists {
		return fmt.Errorf("subnet pool not found for TOR %s", tor.ID)
	}

	// 释放 IP
	return subnetPool.ReleaseIP(ip)
}

// GetPoolStats 获取池统计信息
func (p *TopologyAwarePool) GetPoolStats() *TopologyPoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := &TopologyPoolStats{
		TORStats: make(map[string]topology.SubnetPoolStats),
	}

	// 拓扑统计
	topoStats := p.topology.GetTopologyStats()
	stats.ZoneCount = topoStats.ZoneCount
	stats.PodCount = topoStats.PodCount
	stats.TORCount = topoStats.TORCount
	stats.NodeCount = topoStats.NodeCount

	// 网段池统计
	for torID, pool := range p.subnetPools {
		poolStats := pool.GetStats()
		stats.TORStats[torID] = poolStats

		stats.TotalSubnets += poolStats.SubnetCount
		stats.TotalCapacity += poolStats.TotalCapacity
		stats.TotalUsed += poolStats.TotalUsed
		stats.TotalAvailable += poolStats.TotalAvailable
	}

	if stats.TotalCapacity > 0 {
		stats.UsageRate = float64(stats.TotalUsed) / float64(stats.TotalCapacity)
	}

	return stats
}

// GetNodeStats 获取节点统计信息
func (p *TopologyAwarePool) GetNodeStats(nodeID string) (*NodeStatsDetail, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 获取节点所属 TOR
	tor, err := p.topology.GetNodeTOR(nodeID)
	if err != nil {
		return nil, err
	}

	// 获取节点路径
	path, _ := p.topology.GetNodePath(nodeID)

	// 获取 TOR 的网段池统计
	subnetPool, exists := p.subnetPools[tor.ID]
	if !exists {
		return nil, fmt.Errorf("subnet pool not found for TOR %s", tor.ID)
	}

	poolStats := subnetPool.GetStats()

	// 统计节点在各网段的分配数
	allocations := subnetPool.ListAllocations()
	nodeAllocations := 0
	for _, alloc := range allocations {
		if alloc.NodeID == nodeID {
			nodeAllocations++
		}
	}

	return &NodeStatsDetail{
		NodeID:          nodeID,
		Path:            path,
		TORID:           tor.ID,
		AllocatedIPs:    nodeAllocations,
		TORCapacity:     poolStats.TotalCapacity,
		TORUsed:         poolStats.TotalUsed,
		TORAvailable:    poolStats.TotalAvailable,
		SubnetStats:     poolStats.SubnetStats,
	}, nil
}

// AddSubnetToTOR 为 TOR 添加新的网段
// 用于动态扩展网段
func (p *TopologyAwarePool) AddSubnetToTOR(torID, cidr, purpose string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 验证 TOR 存在
	_, exists := p.subnetPools[torID]
	if !exists {
		return fmt.Errorf("TOR %s not found", torID)
	}

	// 添加到网段池
	subnetPool := p.subnetPools[torID]
	if err := subnetPool.AddSubnet(cidr, purpose); err != nil {
		return err
	}

	// 添加到拓扑
	return p.topology.AddSubnetToTOR(torID, cidr)
}

// GetTopology 返回拓扑管理器（只读访问）
func (p *TopologyAwarePool) GetTopology() *topology.Topology {
	return p.topology
}

// TopologyConfig 拓扑配置
type TopologyConfig struct {
	Zones []ZoneConfig `json:"zones"`
}

// ZoneConfig Zone 配置
type ZoneConfig struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	SubnetRanges []string     `json:"subnet_ranges"`
	Pods         []PodConfig  `json:"pods"`
}

// PodConfig Pod 配置
type PodConfig struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	SubnetRanges []string     `json:"subnet_ranges"`
	TORs         []TORConfig  `json:"tors"`
}

// TORConfig TOR 配置
type TORConfig struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Location string         `json:"location"`
	Subnets  []SubnetConfig `json:"subnets"`
}

// SubnetConfig 网段配置
type SubnetConfig struct {
	CIDR    string `json:"cidr"`
	Purpose string `json:"purpose"`
}

// TopologyPoolStats 拓扑池统计信息
type TopologyPoolStats struct {
	// 拓扑统计
	ZoneCount int
	PodCount  int
	TORCount  int
	NodeCount int

	// 网段统计
	TotalSubnets   int
	TotalCapacity  int
	TotalUsed      int
	TotalAvailable int
	UsageRate      float64

	// TOR 级别统计
	TORStats map[string]topology.SubnetPoolStats
}

// NodeStatsDetail 节点详细统计
type NodeStatsDetail struct {
	NodeID       string
	Path         string // Zone/Pod/TOR/Node
	TORID        string
	AllocatedIPs int

	// TOR 级别统计
	TORCapacity  int
	TORUsed      int
	TORAvailable int

	// 网段级别统计
	SubnetStats map[string]topology.SubnetStats
}
