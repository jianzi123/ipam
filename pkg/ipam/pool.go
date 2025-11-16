package ipam

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/jianzi123/ipam/pkg/allocator"
)

var (
	ErrNodeNotFound    = errors.New("node not found")
	ErrBlockNotFound   = errors.New("block not found")
	ErrCIDRExhausted   = errors.New("cluster CIDR exhausted")
	ErrInvalidCIDR     = errors.New("invalid CIDR")
	ErrBlockInUse      = errors.New("block still has allocated IPs")
	ErrDuplicateBlock  = errors.New("block already exists")
)

// Pool manages IP blocks for all nodes in the cluster
type Pool struct {
	clusterCIDR *net.IPNet
	blockSize   int // CIDR prefix length for each block (e.g., 24 for /24)

	// nodeBlocks maps node ID to list of IP blocks
	nodeBlocks map[string][]*allocator.IPBlock

	// allocatedBlocks tracks all allocated blocks to avoid conflicts
	allocatedBlocks map[string]bool // CIDR string -> true

	mu sync.RWMutex
}

// PoolConfig holds configuration for IP pool
type PoolConfig struct {
	ClusterCIDR string // e.g., "10.244.0.0/16"
	BlockSize   int    // e.g., 24 for /24 blocks
}

// NewPool creates a new IP pool
func NewPool(config PoolConfig) (*Pool, error) {
	_, cidr, err := net.ParseCIDR(config.ClusterCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid cluster CIDR: %w", err)
	}

	// Validate block size
	ones, bits := cidr.Mask.Size()
	if config.BlockSize <= ones || config.BlockSize > bits {
		return nil, fmt.Errorf("invalid block size %d for CIDR /%d", config.BlockSize, ones)
	}

	return &Pool{
		clusterCIDR:     cidr,
		blockSize:       config.BlockSize,
		nodeBlocks:      make(map[string][]*allocator.IPBlock),
		allocatedBlocks: make(map[string]bool),
	}, nil
}

// AllocateBlockForNode allocates a new IP block for a node
// Returns the allocated block
func (p *Pool) AllocateBlockForNode(nodeID string) (*allocator.IPBlock, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find next available block CIDR
	blockCIDR, err := p.findAvailableBlock()
	if err != nil {
		return nil, err
	}

	// Create IP block
	block, err := allocator.NewIPBlock(blockCIDR, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to create IP block: %w", err)
	}

	// Add to node blocks
	p.nodeBlocks[nodeID] = append(p.nodeBlocks[nodeID], block)
	p.allocatedBlocks[blockCIDR] = true

	return block, nil
}

// ReleaseBlockForNode releases an IP block from a node
func (p *Pool) ReleaseBlockForNode(nodeID string, blockCIDR string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	blocks, exists := p.nodeBlocks[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Find and remove the block
	var foundIdx = -1
	var foundBlock *allocator.IPBlock
	for i, block := range blocks {
		if block.CIDR.String() == blockCIDR {
			foundIdx = i
			foundBlock = block
			break
		}
	}

	if foundIdx == -1 {
		return ErrBlockNotFound
	}

	// Check if block has allocated IPs
	if foundBlock.Used > 0 {
		return ErrBlockInUse
	}

	// Remove from slice
	p.nodeBlocks[nodeID] = append(blocks[:foundIdx], blocks[foundIdx+1:]...)
	delete(p.allocatedBlocks, blockCIDR)

	return nil
}

// GetNodeBlocks returns all blocks allocated to a node
func (p *Pool) GetNodeBlocks(nodeID string) ([]*allocator.IPBlock, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	blocks, exists := p.nodeBlocks[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	// Return a copy to avoid external modification
	result := make([]*allocator.IPBlock, len(blocks))
	copy(result, blocks)
	return result, nil
}

// AllocateIPForNode allocates an IP for a pod on a node
// Tries to allocate from existing blocks, creates new block if needed
func (p *Pool) AllocateIPForNode(nodeID string) (net.IP, *allocator.IPBlock, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Get or create blocks for node
	blocks, exists := p.nodeBlocks[nodeID]
	if !exists || len(blocks) == 0 {
		// Create first block for node
		blockCIDR, err := p.findAvailableBlock()
		if err != nil {
			return nil, nil, err
		}

		block, err := allocator.NewIPBlock(blockCIDR, nodeID)
		if err != nil {
			return nil, nil, err
		}

		p.nodeBlocks[nodeID] = []*allocator.IPBlock{block}
		p.allocatedBlocks[blockCIDR] = true
		blocks = p.nodeBlocks[nodeID]
	}

	// Try to allocate from existing blocks
	for _, block := range blocks {
		if ip, err := block.Allocate(); err == nil {
			return ip, block, nil
		}
	}

	// All blocks are full, allocate new block
	blockCIDR, err := p.findAvailableBlock()
	if err != nil {
		return nil, nil, err
	}

	block, err := allocator.NewIPBlock(blockCIDR, nodeID)
	if err != nil {
		return nil, nil, err
	}

	p.nodeBlocks[nodeID] = append(p.nodeBlocks[nodeID], block)
	p.allocatedBlocks[blockCIDR] = true

	// Allocate from new block
	ip, err := block.Allocate()
	if err != nil {
		return nil, nil, err
	}

	return ip, block, nil
}

// ReleaseIP releases an IP address
// Searches all blocks to find which one contains the IP
func (p *Pool) ReleaseIP(ip net.IP, nodeID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	blocks, exists := p.nodeBlocks[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Find which block contains this IP
	for _, block := range blocks {
		if block.CIDR.Contains(ip) {
			return block.Release(ip)
		}
	}

	return ErrBlockNotFound
}

// GetStats returns pool statistics
func (p *Pool) GetStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		TotalNodes:  len(p.nodeBlocks),
		TotalBlocks: len(p.allocatedBlocks),
		NodeStats:   make(map[string]NodeStats),
	}

	for nodeID, blocks := range p.nodeBlocks {
		nodeStats := NodeStats{
			NodeID: nodeID,
			Blocks: len(blocks),
		}

		for _, block := range blocks {
			nodeStats.TotalIPs += block.Total
			nodeStats.UsedIPs += block.Used
			nodeStats.AvailableIPs += block.Available()
		}

		stats.NodeStats[nodeID] = nodeStats
		stats.TotalIPs += nodeStats.TotalIPs
		stats.UsedIPs += nodeStats.UsedIPs
		stats.AvailableIPs += nodeStats.AvailableIPs
	}

	return stats
}

// findAvailableBlock finds next available block CIDR within cluster CIDR
// Must be called with lock held
func (p *Pool) findAvailableBlock() (string, error) {
	// Calculate how many blocks can fit in cluster CIDR
	clusterOnes, bits := p.clusterCIDR.Mask.Size()
	blocksCount := 1 << (p.blockSize - clusterOnes)

	// Get base IP as uint32
	baseIP := p.clusterCIDR.IP.To4()
	if baseIP == nil {
		return "", fmt.Errorf("IPv6 not supported yet")
	}

	baseAddr := uint32(baseIP[0])<<24 | uint32(baseIP[1])<<16 |
		uint32(baseIP[2])<<8 | uint32(baseIP[3])

	// Size of each block
	blockSize := uint32(1 << (bits - p.blockSize))

	// Try each possible block
	for i := 0; i < blocksCount; i++ {
		addr := baseAddr + uint32(i)*blockSize
		ip := net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr))

		cidr := fmt.Sprintf("%s/%d", ip.String(), p.blockSize)

		// Check if already allocated
		if !p.allocatedBlocks[cidr] {
			return cidr, nil
		}
	}

	return "", ErrCIDRExhausted
}

// PoolStats contains pool statistics
type PoolStats struct {
	TotalNodes     int
	TotalBlocks    int
	TotalIPs       int
	UsedIPs        int
	AvailableIPs   int
	NodeStats      map[string]NodeStats
}

// NodeStats contains per-node statistics
type NodeStats struct {
	NodeID       string
	Blocks       int
	TotalIPs     int
	UsedIPs      int
	AvailableIPs int
}

// String returns string representation of stats
func (s PoolStats) String() string {
	return fmt.Sprintf("Pool{Nodes: %d, Blocks: %d, IPs: %d/%d (%.1f%% used)}",
		s.TotalNodes, s.TotalBlocks, s.UsedIPs, s.TotalIPs,
		float64(s.UsedIPs)/float64(s.TotalIPs)*100)
}
