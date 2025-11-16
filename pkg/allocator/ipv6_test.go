package allocator

import (
	"testing"
)

func TestIPv6Block(t *testing.T) {
	t.Run("Create IPv6 block", func(t *testing.T) {
		block, err := NewIPv6Block("2001:db8::/64", "node1")
		if err != nil {
			t.Fatalf("Failed to create IPv6 block: %v", err)
		}

		if block.NodeID != "node1" {
			t.Errorf("Expected node1, got %s", block.NodeID)
		}

		if block.Total <= 0 {
			t.Errorf("Expected positive total, got %d", block.Total)
		}
	})

	t.Run("Allocate and release IPv6", func(t *testing.T) {
		// Use smaller block for testing
		block, err := NewIPv6Block("2001:db8::/120", "node1")
		if err != nil {
			t.Fatalf("Failed to create IPv6 block: %v", err)
		}

		// Allocate first IP
		ip1, err := block.Allocate()
		if err != nil {
			t.Fatalf("Failed to allocate: %v", err)
		}

		if ip1 == nil {
			t.Fatal("Expected non-nil IP")
		}

		// Verify it's in the block
		if !block.CIDR.Contains(ip1) {
			t.Errorf("IP %s not in block %s", ip1, block.CIDR)
		}

		// Allocate second IP
		ip2, err := block.Allocate()
		if err != nil {
			t.Fatalf("Failed to allocate second IP: %v", err)
		}

		// IPs should be different
		if ip1.Equal(ip2) {
			t.Error("Expected different IPs")
		}

		// Release first IP
		if err := block.Release(ip1); err != nil {
			t.Fatalf("Failed to release: %v", err)
		}

		// Usage should decrease
		if block.Used != 1 {
			t.Errorf("Expected used count 1, got %d", block.Used)
		}
	})

	t.Run("IPv6 position conversion", func(t *testing.T) {
		block, _ := NewIPv6Block("2001:db8::/120", "node1")

		// Test position 0
		ip0 := block.positionToIPv6(0)
		if !block.CIDR.IP.Equal(ip0) {
			t.Errorf("Position 0 should equal network IP")
		}

		// Test position 1
		ip1 := block.positionToIPv6(1)
		pos1 := block.ipv6ToPosition(ip1)
		if pos1 != 1 {
			t.Errorf("Expected position 1, got %d", pos1)
		}
	})

	t.Run("Large IPv6 block", func(t *testing.T) {
		// /64 is standard for IPv6
		block, err := NewIPv6Block("2001:db8::/64", "node1")
		if err != nil {
			t.Fatalf("Failed to create large IPv6 block: %v", err)
		}

		// Should still be able to allocate
		ip, err := block.Allocate()
		if err != nil {
			t.Fatalf("Failed to allocate from large block: %v", err)
		}

		if ip == nil {
			t.Fatal("Expected non-nil IP")
		}
	})
}

func TestDualStackBlock(t *testing.T) {
	t.Run("Create dual-stack block", func(t *testing.T) {
		dsb, err := NewDualStackBlock("10.244.1.0/24", "2001:db8::/120", "node1")
		if err != nil {
			t.Fatalf("Failed to create dual-stack block: %v", err)
		}

		if dsb.NodeID != "node1" {
			t.Errorf("Expected node1, got %s", dsb.NodeID)
		}

		if dsb.IPv4Block == nil {
			t.Error("Expected non-nil IPv4 block")
		}

		if dsb.IPv6Block == nil {
			t.Error("Expected non-nil IPv6 block")
		}
	})

	t.Run("Allocate dual-stack IPs", func(t *testing.T) {
		dsb, _ := NewDualStackBlock("10.244.1.0/24", "2001:db8::/120", "node1")

		ipv4, ipv6, err := dsb.AllocateDualStack()
		if err != nil {
			t.Fatalf("Failed to allocate dual-stack: %v", err)
		}

		// Verify IPv4
		if ipv4.To4() == nil {
			t.Error("Expected valid IPv4 address")
		}

		// Verify IPv6
		if ipv6.To4() != nil {
			t.Error("Expected IPv6 address, got IPv4")
		}

		// Both should be in their respective blocks
		if !dsb.IPv4Block.CIDR.Contains(ipv4) {
			t.Errorf("IPv4 %s not in block", ipv4)
		}

		if !dsb.IPv6Block.CIDR.Contains(ipv6) {
			t.Errorf("IPv6 %s not in block", ipv6)
		}
	})

	t.Run("Release dual-stack IPs", func(t *testing.T) {
		dsb, _ := NewDualStackBlock("10.244.1.0/24", "2001:db8::/120", "node1")

		ipv4, ipv6, _ := dsb.AllocateDualStack()

		// Release both
		err := dsb.ReleaseDualStack(ipv4, ipv6)
		if err != nil {
			t.Fatalf("Failed to release dual-stack: %v", err)
		}

		// Both blocks should have usage decreased
		if dsb.IPv4Block.Used != 0 {
			t.Errorf("Expected IPv4 used 0, got %d", dsb.IPv4Block.Used)
		}

		if dsb.IPv6Block.Used != 0 {
			t.Errorf("Expected IPv6 used 0, got %d", dsb.IPv6Block.Used)
		}
	})

	t.Run("IPv6 allocation failure rollback", func(t *testing.T) {
		// Create block with very small IPv6 space
		dsb, _ := NewDualStackBlock("10.244.1.0/24", "2001:db8::/127", "node1")

		// Allocate until IPv6 exhausted (only 2 IPs in /127)
		dsb.AllocateDualStack()
		dsb.AllocateDualStack()

		// Next allocation should fail and rollback IPv4
		initialIPv4Used := dsb.IPv4Block.Used

		_, _, err := dsb.AllocateDualStack()
		if err == nil {
			t.Error("Expected allocation to fail when IPv6 exhausted")
		}

		// IPv4 usage should not increase (rollback)
		if dsb.IPv4Block.Used != initialIPv4Used {
			t.Error("IPv4 allocation was not rolled back")
		}
	})
}

func BenchmarkIPv6Allocate(b *testing.B) {
	block, _ := NewIPv6Block("2001:db8::/120", "node1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip, err := block.Allocate()
		if err != nil {
			block.Release(ip) // Release to make room
			ip, _ = block.Allocate()
		}
		if i%100 == 0 {
			block.Release(ip)
		}
	}
}
