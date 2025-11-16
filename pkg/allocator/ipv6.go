package allocator

import (
	"fmt"
	"math/big"
	"net"
	"time"
)

// IPv6Block represents an IPv6 address block allocated to a node
type IPv6Block struct {
	CIDR      *net.IPNet
	NodeID    string
	Total     int64 // Total usable IPs (can be very large for IPv6)
	Used      int64
	CreatedAt time.Time
	bitmap    *Bitmap // For small blocks, or use a different strategy
}

// NewIPv6Block creates a new IPv6 block from CIDR
func NewIPv6Block(cidr string, nodeID string) (*IPv6Block, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	// Ensure it's IPv6
	if ipNet.IP.To4() != nil {
		return nil, fmt.Errorf("not an IPv6 CIDR: %s", cidr)
	}

	ones, bits := ipNet.Mask.Size()
	if ones >= bits {
		return nil, fmt.Errorf("invalid IPv6 CIDR mask: %s", cidr)
	}

	// Calculate total IPs
	// For IPv6, this can be enormous (e.g., /64 has 2^64 addresses)
	// We'll use a practical limit or different allocation strategy
	var total int64
	maskSize := bits - ones

	if maskSize > 32 {
		// For large blocks (e.g., /64), we can't track individual IPs with bitmap
		// Use a different strategy (sequential allocation, range-based, etc.)
		total = 1 << 32 // Limit to 4 billion for practical purposes
	} else {
		total = 1 << maskSize
	}

	// For now, we'll use bitmap for blocks up to /96 (32-bit address space)
	var bitmap *Bitmap
	if maskSize <= 32 {
		bitmap = NewBitmap(int(total))
	}

	return &IPv6Block{
		CIDR:      ipNet,
		NodeID:    nodeID,
		Total:     total,
		Used:      0,
		CreatedAt: time.Now(),
		bitmap:    bitmap,
	}, nil
}

// Allocate allocates an IPv6 address from the block
func (block *IPv6Block) Allocate() (net.IP, error) {
	if block.bitmap != nil {
		// Use bitmap for small blocks
		pos := block.bitmap.FindFirstZero()
		if pos == -1 {
			return nil, ErrNoAvailableIP
		}

		if err := block.bitmap.Set(pos); err != nil {
			return nil, err
		}

		ip := block.positionToIPv6(int64(pos))
		block.Used++
		return ip, nil
	}

	// For large blocks, use sequential allocation
	// This is a simplified approach; production would need better tracking
	if block.Used >= block.Total {
		return nil, ErrNoAvailableIP
	}

	ip := block.positionToIPv6(block.Used)
	block.Used++
	return ip, nil
}

// Release releases an IPv6 address back to the block
func (block *IPv6Block) Release(ip net.IP) error {
	if !block.CIDR.Contains(ip) {
		return ErrIPNotInBlock
	}

	if block.bitmap != nil {
		pos := block.ipv6ToPosition(ip)
		if pos < 0 || pos >= int64(block.bitmap.size) {
			return ErrInvalidIP
		}

		if err := block.bitmap.Clear(int(pos)); err != nil {
			return err
		}

		block.Used--
		return nil
	}

	// For large blocks without bitmap, we'd need a different tracking mechanism
	// This is simplified
	if block.Used > 0 {
		block.Used--
	}
	return nil
}

// positionToIPv6 converts position to IPv6 address
func (block *IPv6Block) positionToIPv6(pos int64) net.IP {
	// Get network address as big.Int
	networkIP := block.CIDR.IP

	// Convert network IP to big.Int
	networkInt := new(big.Int).SetBytes(networkIP)

	// Add position
	posInt := big.NewInt(pos)
	resultInt := new(big.Int).Add(networkInt, posInt)

	// Convert back to IP
	ipBytes := resultInt.Bytes()

	// Ensure 16 bytes for IPv6
	if len(ipBytes) < 16 {
		padded := make([]byte, 16)
		copy(padded[16-len(ipBytes):], ipBytes)
		ipBytes = padded
	}

	return net.IP(ipBytes)
}

// ipv6ToPosition converts IPv6 address to position
func (block *IPv6Block) ipv6ToPosition(ip net.IP) int64 {
	networkInt := new(big.Int).SetBytes(block.CIDR.IP)
	ipInt := new(big.Int).SetBytes(ip.To16())

	posInt := new(big.Int).Sub(ipInt, networkInt)

	// For safety, limit to int64 range
	if !posInt.IsInt64() {
		return -1
	}

	return posInt.Int64()
}

// Available returns number of available IPs
func (block *IPv6Block) Available() int64 {
	return block.Total - block.Used
}

// Usage returns the usage ratio (0.0 to 1.0)
func (block *IPv6Block) Usage() float64 {
	if block.Total == 0 {
		return 0
	}
	return float64(block.Used) / float64(block.Total)
}

// String returns string representation
func (block *IPv6Block) String() string {
	return fmt.Sprintf("IPv6Block{CIDR: %s, Node: %s, Used: %d/%d, Usage: %.1f%%}",
		block.CIDR.String(), block.NodeID, block.Used, block.Total, block.Usage()*100)
}

// DualStackBlock represents both IPv4 and IPv6 blocks for a node
type DualStackBlock struct {
	IPv4Block *IPBlock
	IPv6Block *IPv6Block
	NodeID    string
}

// NewDualStackBlock creates a new dual-stack block
func NewDualStackBlock(ipv4CIDR, ipv6CIDR, nodeID string) (*DualStackBlock, error) {
	ipv4Block, err := NewIPBlock(ipv4CIDR, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPv4 block: %w", err)
	}

	ipv6Block, err := NewIPv6Block(ipv6CIDR, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPv6 block: %w", err)
	}

	return &DualStackBlock{
		IPv4Block: ipv4Block,
		IPv6Block: ipv6Block,
		NodeID:    nodeID,
	}, nil
}

// AllocateDualStack allocates both IPv4 and IPv6 addresses
func (dsb *DualStackBlock) AllocateDualStack() (ipv4, ipv6 net.IP, err error) {
	ipv4, err = dsb.IPv4Block.Allocate()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to allocate IPv4: %w", err)
	}

	ipv6, err = dsb.IPv6Block.Allocate()
	if err != nil {
		// Rollback IPv4 allocation
		dsb.IPv4Block.Release(ipv4)
		return nil, nil, fmt.Errorf("failed to allocate IPv6: %w", err)
	}

	return ipv4, ipv6, nil
}

// ReleaseDualStack releases both IPv4 and IPv6 addresses
func (dsb *DualStackBlock) ReleaseDualStack(ipv4, ipv6 net.IP) error {
	err4 := dsb.IPv4Block.Release(ipv4)
	err6 := dsb.IPv6Block.Release(ipv6)

	if err4 != nil {
		return err4
	}
	if err6 != nil {
		return err6
	}

	return nil
}
