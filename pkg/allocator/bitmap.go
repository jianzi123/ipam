package allocator

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

var (
	ErrNoAvailableIP = errors.New("no available IP in the block")
	ErrInvalidIP     = errors.New("invalid IP address")
	ErrIPNotInBlock  = errors.New("IP not in this block")
)

// IPBlock represents an IP address block allocated to a node
type IPBlock struct {
	CIDR      *net.IPNet
	NodeID    string
	Total     int       // Total usable IPs
	Used      int       // Used IP count
	CreatedAt time.Time
	bitmap    *Bitmap   // Internal bitmap for fast allocation
	mu        sync.RWMutex
}

// Bitmap represents a bitmap for IP allocation
// Uses uint64 array for efficient bit operations
type Bitmap struct {
	bits      []uint64
	size      int // Total number of bits
	allocated int // Number of allocated bits
}

// NewBitmap creates a new bitmap with given size
func NewBitmap(size int) *Bitmap {
	// Calculate number of uint64 needed
	n := (size + 63) / 64
	return &Bitmap{
		bits:      make([]uint64, n),
		size:      size,
		allocated: 0,
	}
}

// Set marks a bit as allocated
func (b *Bitmap) Set(pos int) error {
	if pos < 0 || pos >= b.size {
		return fmt.Errorf("position %d out of range [0, %d)", pos, b.size)
	}

	idx := pos / 64
	bit := uint(pos % 64)

	// Check if already set
	if b.bits[idx]&(1<<bit) != 0 {
		return fmt.Errorf("bit %d already set", pos)
	}

	b.bits[idx] |= (1 << bit)
	b.allocated++
	return nil
}

// Clear marks a bit as free
func (b *Bitmap) Clear(pos int) error {
	if pos < 0 || pos >= b.size {
		return fmt.Errorf("position %d out of range [0, %d)", pos, b.size)
	}

	idx := pos / 64
	bit := uint(pos % 64)

	// Check if already clear
	if b.bits[idx]&(1<<bit) == 0 {
		return fmt.Errorf("bit %d already clear", pos)
	}

	b.bits[idx] &^= (1 << bit)
	b.allocated--
	return nil
}

// IsSet checks if a bit is allocated
func (b *Bitmap) IsSet(pos int) bool {
	if pos < 0 || pos >= b.size {
		return false
	}

	idx := pos / 64
	bit := uint(pos % 64)
	return b.bits[idx]&(1<<bit) != 0
}

// FindFirstZero finds the first unallocated bit
// Returns -1 if no free bit is found
func (b *Bitmap) FindFirstZero() int {
	for i := 0; i < len(b.bits); i++ {
		if b.bits[i] != ^uint64(0) { // Not all bits are set
			// Find first zero bit in this uint64
			for j := 0; j < 64; j++ {
				pos := i*64 + j
				if pos >= b.size {
					return -1
				}
				if !b.IsSet(pos) {
					return pos
				}
			}
		}
	}
	return -1
}

// Count returns number of allocated bits
func (b *Bitmap) Count() int {
	return b.allocated
}

// Available returns number of free bits
func (b *Bitmap) Available() int {
	return b.size - b.allocated
}

// NewIPBlock creates a new IP block from CIDR
func NewIPBlock(cidr string, nodeID string) (*IPBlock, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	// Calculate total usable IPs
	// For /24: 256 total, 254 usable (excluding network and broadcast)
	ones, bits := ipNet.Mask.Size()
	total := 1 << (bits - ones)

	// Reserve first (network) and last (broadcast) addresses
	usable := total - 2
	if usable <= 0 {
		return nil, fmt.Errorf("CIDR %s has no usable IPs", cidr)
	}

	return &IPBlock{
		CIDR:      ipNet,
		NodeID:    nodeID,
		Total:     usable,
		Used:      0,
		CreatedAt: time.Now(),
		bitmap:    NewBitmap(usable),
	}, nil
}

// Allocate allocates an IP from the block
// Returns the allocated IP address
func (block *IPBlock) Allocate() (net.IP, error) {
	block.mu.Lock()
	defer block.mu.Unlock()

	// Find first available position
	pos := block.bitmap.FindFirstZero()
	if pos == -1 {
		return nil, ErrNoAvailableIP
	}

	// Mark as allocated
	if err := block.bitmap.Set(pos); err != nil {
		return nil, err
	}

	// Convert position to IP
	// Position 0 = network address + 1
	ip := block.positionToIP(pos)
	block.Used++

	return ip, nil
}

// Release releases an IP back to the block
func (block *IPBlock) Release(ip net.IP) error {
	block.mu.Lock()
	defer block.mu.Unlock()

	// Check if IP belongs to this block
	if !block.CIDR.Contains(ip) {
		return ErrIPNotInBlock
	}

	// Convert IP to position
	pos := block.ipToPosition(ip)
	if pos < 0 || pos >= block.Total {
		return ErrInvalidIP
	}

	// Clear the bit
	if err := block.bitmap.Clear(pos); err != nil {
		return err
	}

	block.Used--
	return nil
}

// Contains checks if an IP is in this block and allocated
func (block *IPBlock) Contains(ip net.IP) bool {
	block.mu.RLock()
	defer block.mu.RUnlock()

	if !block.CIDR.Contains(ip) {
		return false
	}

	pos := block.ipToPosition(ip)
	return block.bitmap.IsSet(pos)
}

// Available returns number of available IPs
func (block *IPBlock) Available() int {
	block.mu.RLock()
	defer block.mu.RUnlock()
	return block.bitmap.Available()
}

// Usage returns the usage ratio (0.0 to 1.0)
func (block *IPBlock) Usage() float64 {
	block.mu.RLock()
	defer block.mu.RUnlock()

	if block.Total == 0 {
		return 0
	}
	return float64(block.Used) / float64(block.Total)
}

// positionToIP converts bitmap position to IP address
// Position 0 = first usable IP (network address + 1)
func (block *IPBlock) positionToIP(pos int) net.IP {
	// Get network address as uint32
	ip := block.CIDR.IP.To4()
	if ip == nil {
		// IPv6 not supported yet
		return nil
	}

	addr := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	// Add position + 1 (skip network address)
	addr += uint32(pos + 1)

	return net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr))
}

// ipToPosition converts IP address to bitmap position
func (block *IPBlock) ipToPosition(ip net.IP) int {
	networkIP := block.CIDR.IP.To4()
	targetIP := ip.To4()

	if networkIP == nil || targetIP == nil {
		return -1
	}

	networkAddr := uint32(networkIP[0])<<24 | uint32(networkIP[1])<<16 |
		uint32(networkIP[2])<<8 | uint32(networkIP[3])
	targetAddr := uint32(targetIP[0])<<24 | uint32(targetIP[1])<<16 |
		uint32(targetIP[2])<<8 | uint32(targetIP[3])

	// Position = offset - 1 (network address is reserved)
	offset := int(targetAddr - networkAddr)
	return offset - 1
}

// String returns string representation
func (block *IPBlock) String() string {
	block.mu.RLock()
	defer block.mu.RUnlock()

	return fmt.Sprintf("IPBlock{CIDR: %s, Node: %s, Used: %d/%d, Usage: %.1f%%}",
		block.CIDR.String(), block.NodeID, block.Used, block.Total, block.Usage()*100)
}
