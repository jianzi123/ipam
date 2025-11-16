package topology

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jianzi123/ipam/pkg/allocator"
)

// SubnetPool 管理一个 TOR 的网段池
type SubnetPool struct {
	TORID   string
	Subnets map[string]*Subnet // CIDR -> Subnet
	mu      sync.RWMutex
}

// Subnet 代表一个 CIDR 网段
type Subnet struct {
	CIDR        *net.IPNet
	Purpose     string // "default", "storage", "management"
	Capacity    int
	Used        int
	Allocations map[string]*IPAllocation // IP -> Allocation
	bitmap      *allocator.Bitmap
	mu          sync.RWMutex
}

// IPAllocation 记录单个 IP 的分配信息
type IPAllocation struct {
	IP          string
	NodeID      string
	ContainerID string
	PodName     string
	Namespace   string
	AllocatedAt time.Time
}

// NewSubnetPool 创建一个新的网段池
func NewSubnetPool(torID string) *SubnetPool {
	return &SubnetPool{
		TORID:   torID,
		Subnets: make(map[string]*Subnet),
	}
}

// AddSubnet 添加一个网段到池中
func (sp *SubnetPool) AddSubnet(cidr, purpose string) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if _, exists := sp.Subnets[cidr]; exists {
		return fmt.Errorf("subnet %s already exists in pool", cidr)
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	// 计算容量
	ones, bits := ipNet.Mask.Size()
	total := 1 << (bits - ones)
	usable := total - 2 // 减去网络地址和广播地址

	if usable <= 0 {
		return fmt.Errorf("CIDR %s has no usable IPs", cidr)
	}

	subnet := &Subnet{
		CIDR:        ipNet,
		Purpose:     purpose,
		Capacity:    usable,
		Used:        0,
		Allocations: make(map[string]*IPAllocation),
		bitmap:      allocator.NewBitmap(usable),
	}

	sp.Subnets[cidr] = subnet
	return nil
}

// AllocateIP 从池中分配一个 IP
func (sp *SubnetPool) AllocateIP(nodeID, purpose string) (net.IP, string, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// 选择合适的网段
	var targetSubnet *Subnet
	var targetCIDR string

	// 策略1: 优先选择指定用途且有空闲IP的网段
	for cidr, subnet := range sp.Subnets {
		if purpose != "" && subnet.Purpose != purpose {
			continue
		}

		subnet.mu.RLock()
		available := subnet.Capacity - subnet.Used
		subnet.mu.RUnlock()

		if available > 0 {
			targetSubnet = subnet
			targetCIDR = cidr
			break
		}
	}

	// 策略2: 如果没有指定用途的网段，使用默认网段
	if targetSubnet == nil && purpose != "default" {
		for cidr, subnet := range sp.Subnets {
			if subnet.Purpose == "default" {
				subnet.mu.RLock()
				available := subnet.Capacity - subnet.Used
				subnet.mu.RUnlock()

				if available > 0 {
					targetSubnet = subnet
					targetCIDR = cidr
					break
				}
			}
		}
	}

	if targetSubnet == nil {
		return nil, "", fmt.Errorf("no available subnet in pool for purpose %s", purpose)
	}

	// 从选定的网段分配 IP
	targetSubnet.mu.Lock()
	defer targetSubnet.mu.Unlock()

	pos := targetSubnet.bitmap.FindFirstZero()
	if pos == -1 {
		return nil, "", fmt.Errorf("no available IP in subnet %s", targetCIDR)
	}

	if err := targetSubnet.bitmap.Set(pos); err != nil {
		return nil, "", err
	}

	// 计算 IP 地址
	ip := positionToIP(targetSubnet.CIDR, pos)
	targetSubnet.Used++

	// 记录分配
	allocation := &IPAllocation{
		IP:          ip.String(),
		NodeID:      nodeID,
		AllocatedAt: time.Now(),
	}
	targetSubnet.Allocations[ip.String()] = allocation

	return ip, targetCIDR, nil
}

// ReleaseIP 释放一个 IP
func (sp *SubnetPool) ReleaseIP(ip net.IP) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// 找到包含这个 IP 的网段
	var targetSubnet *Subnet

	for _, subnet := range sp.Subnets {
		if subnet.CIDR.Contains(ip) {
			targetSubnet = subnet
			break
		}
	}

	if targetSubnet == nil {
		return fmt.Errorf("IP %s not found in any subnet", ip.String())
	}

	targetSubnet.mu.Lock()
	defer targetSubnet.mu.Unlock()

	// 检查是否已分配
	if _, exists := targetSubnet.Allocations[ip.String()]; !exists {
		return fmt.Errorf("IP %s not allocated", ip.String())
	}

	// 计算位置并释放
	pos := ipToPosition(targetSubnet.CIDR, ip)
	if err := targetSubnet.bitmap.Clear(pos); err != nil {
		return err
	}

	targetSubnet.Used--
	delete(targetSubnet.Allocations, ip.String())

	return nil
}

// GetAllocation 获取 IP 的分配信息
func (sp *SubnetPool) GetAllocation(ip net.IP) (*IPAllocation, error) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	for _, subnet := range sp.Subnets {
		if !subnet.CIDR.Contains(ip) {
			continue
		}

		subnet.mu.RLock()
		allocation, exists := subnet.Allocations[ip.String()]
		subnet.mu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("IP %s not allocated", ip.String())
		}

		return allocation, nil
	}

	return nil, fmt.Errorf("IP %s not found in pool", ip.String())
}

// GetStats 获取池的统计信息
func (sp *SubnetPool) GetStats() SubnetPoolStats {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	stats := SubnetPoolStats{
		TORID:        sp.TORID,
		SubnetCount:  len(sp.Subnets),
		SubnetStats:  make(map[string]SubnetStats),
	}

	for cidr, subnet := range sp.Subnets {
		subnet.mu.RLock()
		sStats := SubnetStats{
			CIDR:      cidr,
			Purpose:   subnet.Purpose,
			Capacity:  subnet.Capacity,
			Used:      subnet.Used,
			Available: subnet.Capacity - subnet.Used,
			UsageRate: float64(subnet.Used) / float64(subnet.Capacity),
		}
		subnet.mu.RUnlock()

		stats.SubnetStats[cidr] = sStats
		stats.TotalCapacity += sStats.Capacity
		stats.TotalUsed += sStats.Used
		stats.TotalAvailable += sStats.Available
	}

	if stats.TotalCapacity > 0 {
		stats.UsageRate = float64(stats.TotalUsed) / float64(stats.TotalCapacity)
	}

	return stats
}

// SubnetPoolStats 网段池统计信息
type SubnetPoolStats struct {
	TORID          string
	SubnetCount    int
	TotalCapacity  int
	TotalUsed      int
	TotalAvailable int
	UsageRate      float64
	SubnetStats    map[string]SubnetStats
}

// SubnetStats 单个网段统计信息
type SubnetStats struct {
	CIDR      string
	Purpose   string
	Capacity  int
	Used      int
	Available int
	UsageRate float64
}

// ListAllocations 列出所有分配
func (sp *SubnetPool) ListAllocations() []*IPAllocation {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	allocations := make([]*IPAllocation, 0)

	for _, subnet := range sp.Subnets {
		subnet.mu.RLock()
		for _, allocation := range subnet.Allocations {
			allocations = append(allocations, allocation)
		}
		subnet.mu.RUnlock()
	}

	return allocations
}

// positionToIP 将位置转换为 IP 地址
func positionToIP(cidr *net.IPNet, pos int) net.IP {
	ip := cidr.IP.To4()
	if ip == nil {
		// IPv6 暂不支持
		return nil
	}

	addr := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	addr += uint32(pos + 1) // +1 跳过网络地址

	return net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr))
}

// ipToPosition 将 IP 地址转换为位置
func ipToPosition(cidr *net.IPNet, ip net.IP) int {
	networkIP := cidr.IP.To4()
	targetIP := ip.To4()

	if networkIP == nil || targetIP == nil {
		return -1
	}

	networkAddr := uint32(networkIP[0])<<24 | uint32(networkIP[1])<<16 |
		uint32(networkIP[2])<<8 | uint32(networkIP[3])
	targetAddr := uint32(targetIP[0])<<24 | uint32(targetIP[1])<<16 |
		uint32(targetIP[2])<<8 | uint32(targetIP[3])

	offset := int(targetAddr - networkAddr)
	return offset - 1 // -1 因为位置0对应网络地址+1
}
