package ipam

import (
	"fmt"
	"net"
	"testing"
)

func TestPool(t *testing.T) {
	t.Run("Create pool", func(t *testing.T) {
		config := PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		}

		pool, err := NewPool(config)
		if err != nil {
			t.Fatalf("NewPool failed: %v", err)
		}

		if pool.blockSize != 24 {
			t.Errorf("Expected block size 24, got %d", pool.blockSize)
		}
	})

	t.Run("Allocate block for node", func(t *testing.T) {
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		})

		block, err := pool.AllocateBlockForNode("node1")
		if err != nil {
			t.Fatalf("AllocateBlockForNode failed: %v", err)
		}

		if block.NodeID != "node1" {
			t.Errorf("Expected node1, got %s", block.NodeID)
		}

		if block.Total != 254 {
			t.Errorf("Expected 254 IPs, got %d", block.Total)
		}

		// Allocate another block
		block2, err := pool.AllocateBlockForNode("node1")
		if err != nil {
			t.Fatalf("Second allocation failed: %v", err)
		}

		if block.CIDR.String() == block2.CIDR.String() {
			t.Error("Expected different CIDRs for two blocks")
		}
	})

	t.Run("Allocate IP for node", func(t *testing.T) {
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		})

		ip1, block1, err := pool.AllocateIPForNode("node1")
		if err != nil {
			t.Fatalf("AllocateIPForNode failed: %v", err)
		}

		if ip1 == nil {
			t.Fatal("Expected non-nil IP")
		}

		if !block1.CIDR.Contains(ip1) {
			t.Errorf("IP %s not in block %s", ip1, block1.CIDR)
		}

		// Allocate more IPs
		for i := 0; i < 10; i++ {
			ip, _, err := pool.AllocateIPForNode("node1")
			if err != nil {
				t.Fatalf("Allocation %d failed: %v", i, err)
			}
			if ip == nil {
				t.Fatalf("Got nil IP for allocation %d", i)
			}
		}

		stats := pool.GetStats()
		if stats.UsedIPs != 11 {
			t.Errorf("Expected 11 used IPs, got %d", stats.UsedIPs)
		}
	})

	t.Run("Release IP", func(t *testing.T) {
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		})

		ip, _, err := pool.AllocateIPForNode("node1")
		if err != nil {
			t.Fatalf("Allocate failed: %v", err)
		}

		stats := pool.GetStats()
		if stats.UsedIPs != 1 {
			t.Errorf("Expected 1 used IP, got %d", stats.UsedIPs)
		}

		// Release IP
		if err := pool.ReleaseIP(ip, "node1"); err != nil {
			t.Fatalf("Release failed: %v", err)
		}

		stats = pool.GetStats()
		if stats.UsedIPs != 0 {
			t.Errorf("Expected 0 used IPs after release, got %d", stats.UsedIPs)
		}
	})

	t.Run("Get node blocks", func(t *testing.T) {
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		})

		pool.AllocateBlockForNode("node1")
		pool.AllocateBlockForNode("node1")

		blocks, err := pool.GetNodeBlocks("node1")
		if err != nil {
			t.Fatalf("GetNodeBlocks failed: %v", err)
		}

		if len(blocks) != 2 {
			t.Errorf("Expected 2 blocks, got %d", len(blocks))
		}
	})

	t.Run("Release block", func(t *testing.T) {
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		})

		block, _ := pool.AllocateBlockForNode("node1")
		cidr := block.CIDR.String()

		// Should succeed for empty block
		if err := pool.ReleaseBlockForNode("node1", cidr); err != nil {
			t.Fatalf("ReleaseBlockForNode failed: %v", err)
		}

		blocks, _ := pool.GetNodeBlocks("node1")
		if len(blocks) != 0 {
			t.Errorf("Expected 0 blocks after release, got %d", len(blocks))
		}
	})

	t.Run("Cannot release block with allocated IPs", func(t *testing.T) {
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		})

		block, _ := pool.AllocateBlockForNode("node1")
		block.Allocate() // Allocate an IP

		// Should fail
		err := pool.ReleaseBlockForNode("node1", block.CIDR.String())
		if err != ErrBlockInUse {
			t.Errorf("Expected ErrBlockInUse, got %v", err)
		}
	})

	t.Run("Stats calculation", func(t *testing.T) {
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/16",
			BlockSize:   24,
		})

		// Allocate for two nodes
		for i := 0; i < 5; i++ {
			pool.AllocateIPForNode("node1")
		}
		for i := 0; i < 3; i++ {
			pool.AllocateIPForNode("node2")
		}

		stats := pool.GetStats()

		if stats.TotalNodes != 2 {
			t.Errorf("Expected 2 nodes, got %d", stats.TotalNodes)
		}

		if stats.UsedIPs != 8 {
			t.Errorf("Expected 8 used IPs, got %d", stats.UsedIPs)
		}

		node1Stats := stats.NodeStats["node1"]
		if node1Stats.UsedIPs != 5 {
			t.Errorf("Expected 5 IPs for node1, got %d", node1Stats.UsedIPs)
		}

		node2Stats := stats.NodeStats["node2"]
		if node2Stats.UsedIPs != 3 {
			t.Errorf("Expected 3 IPs for node2, got %d", node2Stats.UsedIPs)
		}
	})

	t.Run("CIDR exhaustion", func(t *testing.T) {
		// Use very small CIDR to test exhaustion
		pool, _ := NewPool(PoolConfig{
			ClusterCIDR: "10.244.0.0/28", // Only 16 IPs total
			BlockSize:   30,               // 4 IPs per block (2 usable)
		})

		// Should be able to allocate 4 blocks (16/4)
		for i := 0; i < 4; i++ {
			_, err := pool.AllocateBlockForNode("node1")
			if err != nil {
				t.Fatalf("Block %d allocation failed: %v", i, err)
			}
		}

		// 5th allocation should fail
		_, err := pool.AllocateBlockForNode("node1")
		if err != ErrCIDRExhausted {
			t.Errorf("Expected ErrCIDRExhausted, got %v", err)
		}
	})
}

func BenchmarkPoolAllocateIP(b *testing.B) {
	pool, _ := NewPool(PoolConfig{
		ClusterCIDR: "10.244.0.0/16",
		BlockSize:   24,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeID := fmt.Sprintf("node%d", i%100) // Simulate 100 nodes
		pool.AllocateIPForNode(nodeID)
	}
}

func BenchmarkPoolReleaseIP(b *testing.B) {
	pool, _ := NewPool(PoolConfig{
		ClusterCIDR: "10.244.0.0/16",
		BlockSize:   24,
	})

	// Pre-allocate some IPs
	ips := make([]struct {
		ip     net.IP
		nodeID string
	}, 1000)

	for i := 0; i < 1000; i++ {
		nodeID := fmt.Sprintf("node%d", i%10)
		ip, _, _ := pool.AllocateIPForNode(nodeID)
		ips[i].ip = ip
		ips[i].nodeID = nodeID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry := ips[i%1000]
		pool.ReleaseIP(entry.ip, entry.nodeID)
		// Re-allocate to keep pool state
		ip, _, _ := pool.AllocateIPForNode(entry.nodeID)
		ips[i%1000].ip = ip
	}
}
