package topology

import (
	"fmt"
	"sync"
	"time"
)

// Zone 代表一个可用区
type Zone struct {
	ID           string
	Name         string
	SubnetRanges []string // e.g., ["10.244.0.0/20"]
	Pods         map[string]*Pod
	CreatedAt    time.Time
	mu           sync.RWMutex
}

// Pod 代表一个机柜组
type Pod struct {
	ID           string
	Name         string
	ZoneID       string
	SubnetRanges []string // e.g., ["10.244.0.0/21"]
	TORs         map[string]*TOR
	CreatedAt    time.Time
	mu           sync.RWMutex
}

// TOR 代表一个 Top of Rack 交换机
type TOR struct {
	ID        string
	Name      string
	PodID     string
	Location  string   // 物理位置，如 "Rack 01"
	Subnets   []string // TOR 拥有的网段列表
	Nodes     []string // 节点 ID 列表
	CreatedAt time.Time
	mu        sync.RWMutex
}

// Node 代表一个节点/宿主机
type Node struct {
	ID        string
	Name      string
	TORID     string
	Labels    map[string]string
	Subnets   []*NodeSubnet
	CreatedAt time.Time
	UpdatedAt time.Time
	mu        sync.RWMutex
}

// NodeSubnet 代表节点使用的网段
type NodeSubnet struct {
	SubnetCIDR   string
	Purpose      string // "default", "storage", "management"
	AllocatedIPs int
	Capacity     int
}

// Topology 管理整个网络拓扑
type Topology struct {
	Zones map[string]*Zone
	Pods  map[string]*Pod
	TORs  map[string]*TOR
	Nodes map[string]*Node
	mu    sync.RWMutex
}

// NewTopology 创建一个新的拓扑管理器
func NewTopology() *Topology {
	return &Topology{
		Zones: make(map[string]*Zone),
		Pods:  make(map[string]*Pod),
		TORs:  make(map[string]*TOR),
		Nodes: make(map[string]*Node),
	}
}

// AddZone 添加一个可用区
func (t *Topology) AddZone(zone *Zone) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.Zones[zone.ID]; exists {
		return fmt.Errorf("zone %s already exists", zone.ID)
	}

	zone.CreatedAt = time.Now()
	if zone.Pods == nil {
		zone.Pods = make(map[string]*Pod)
	}

	t.Zones[zone.ID] = zone
	return nil
}

// AddPod 添加一个机柜组
func (t *Topology) AddPod(pod *Pod) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 验证 Zone 存在
	zone, exists := t.Zones[pod.ZoneID]
	if !exists {
		return fmt.Errorf("zone %s not found", pod.ZoneID)
	}

	if _, exists := t.Pods[pod.ID]; exists {
		return fmt.Errorf("pod %s already exists", pod.ID)
	}

	pod.CreatedAt = time.Now()
	if pod.TORs == nil {
		pod.TORs = make(map[string]*TOR)
	}

	t.Pods[pod.ID] = pod
	zone.Pods[pod.ID] = pod

	return nil
}

// AddTOR 添加一个 TOR 交换机
func (t *Topology) AddTOR(tor *TOR) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 验证 Pod 存在
	pod, exists := t.Pods[tor.PodID]
	if !exists {
		return fmt.Errorf("pod %s not found", tor.PodID)
	}

	if _, exists := t.TORs[tor.ID]; exists {
		return fmt.Errorf("TOR %s already exists", tor.ID)
	}

	tor.CreatedAt = time.Now()
	if tor.Nodes == nil {
		tor.Nodes = make([]string, 0)
	}

	t.TORs[tor.ID] = tor
	pod.TORs[tor.ID] = tor

	return nil
}

// RegisterNode 注册一个节点
func (t *Topology) RegisterNode(node *Node) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 验证 TOR 存在
	tor, exists := t.TORs[node.TORID]
	if !exists {
		return fmt.Errorf("TOR %s not found", node.TORID)
	}

	if _, exists := t.Nodes[node.ID]; exists {
		return fmt.Errorf("node %s already registered", node.ID)
	}

	node.CreatedAt = time.Now()
	node.UpdatedAt = time.Now()
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	if node.Subnets == nil {
		node.Subnets = make([]*NodeSubnet, 0)
	}

	t.Nodes[node.ID] = node

	// 添加到 TOR 的节点列表
	tor.mu.Lock()
	tor.Nodes = append(tor.Nodes, node.ID)
	tor.mu.Unlock()

	return nil
}

// GetNodeTOR 获取节点所属的 TOR
func (t *Topology) GetNodeTOR(nodeID string) (*TOR, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node, exists := t.Nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}

	tor, exists := t.TORs[node.TORID]
	if !exists {
		return nil, fmt.Errorf("TOR %s not found", node.TORID)
	}

	return tor, nil
}

// GetTORSubnets 获取 TOR 的所有网段
func (t *Topology) GetTORSubnets(torID string) ([]string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tor, exists := t.TORs[torID]
	if !exists {
		return nil, fmt.Errorf("TOR %s not found", torID)
	}

	tor.mu.RLock()
	defer tor.mu.RUnlock()

	// 返回副本
	subnets := make([]string, len(tor.Subnets))
	copy(subnets, tor.Subnets)

	return subnets, nil
}

// AddSubnetToTOR 为 TOR 添加一个网段
func (t *Topology) AddSubnetToTOR(torID, subnet string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	tor, exists := t.TORs[torID]
	if !exists {
		return fmt.Errorf("TOR %s not found", torID)
	}

	tor.mu.Lock()
	defer tor.mu.Unlock()

	// 检查是否已存在
	for _, s := range tor.Subnets {
		if s == subnet {
			return fmt.Errorf("subnet %s already exists in TOR %s", subnet, torID)
		}
	}

	tor.Subnets = append(tor.Subnets, subnet)
	return nil
}

// GetTopologyStats 获取拓扑统计信息
func (t *Topology) GetTopologyStats() TopologyStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := TopologyStats{
		ZoneCount: len(t.Zones),
		PodCount:  len(t.Pods),
		TORCount:  len(t.TORs),
		NodeCount: len(t.Nodes),
		ZoneStats: make(map[string]ZoneStats),
	}

	for zoneID, zone := range t.Zones {
		zone.mu.RLock()
		zStats := ZoneStats{
			ZoneID:   zoneID,
			PodCount: len(zone.Pods),
		}

		// 统计 TOR 和节点数
		for _, pod := range zone.Pods {
			pod.mu.RLock()
			zStats.TORCount += len(pod.TORs)

			for _, tor := range pod.TORs {
				tor.mu.RLock()
				zStats.NodeCount += len(tor.Nodes)
				tor.mu.RUnlock()
			}
			pod.mu.RUnlock()
		}

		stats.ZoneStats[zoneID] = zStats
		zone.mu.RUnlock()
	}

	return stats
}

// TopologyStats 拓扑统计信息
type TopologyStats struct {
	ZoneCount int
	PodCount  int
	TORCount  int
	NodeCount int
	ZoneStats map[string]ZoneStats
}

// ZoneStats 可用区统计信息
type ZoneStats struct {
	ZoneID    string
	PodCount  int
	TORCount  int
	NodeCount int
}

// GetNodePath 获取节点的完整路径
func (t *Topology) GetNodePath(nodeID string) (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node, exists := t.Nodes[nodeID]
	if !exists {
		return "", fmt.Errorf("node %s not found", nodeID)
	}

	tor, exists := t.TORs[node.TORID]
	if !exists {
		return "", fmt.Errorf("TOR %s not found", node.TORID)
	}

	pod, exists := t.Pods[tor.PodID]
	if !exists {
		return "", fmt.Errorf("Pod %s not found", tor.PodID)
	}

	zone, exists := t.Zones[pod.ZoneID]
	if !exists {
		return "", fmt.Errorf("Zone %s not found", pod.ZoneID)
	}

	return fmt.Sprintf("%s/%s/%s/%s", zone.Name, pod.Name, tor.Name, node.Name), nil
}

// ListNodesByTOR 列出 TOR 下的所有节点
func (t *Topology) ListNodesByTOR(torID string) ([]*Node, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tor, exists := t.TORs[torID]
	if !exists {
		return nil, fmt.Errorf("TOR %s not found", torID)
	}

	tor.mu.RLock()
	nodeIDs := make([]string, len(tor.Nodes))
	copy(nodeIDs, tor.Nodes)
	tor.mu.RUnlock()

	nodes := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if node, exists := t.Nodes[nodeID]; exists {
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}
