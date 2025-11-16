package allocator

import (
	"net"
	"testing"
)

func TestBitmap(t *testing.T) {
	t.Run("Basic operations", func(t *testing.T) {
		bm := NewBitmap(100)

		// Test Set
		if err := bm.Set(0); err != nil {
			t.Fatalf("Set(0) failed: %v", err)
		}
		if !bm.IsSet(0) {
			t.Error("Expected bit 0 to be set")
		}
		if bm.Count() != 1 {
			t.Errorf("Expected count 1, got %d", bm.Count())
		}

		// Test duplicate Set
		if err := bm.Set(0); err == nil {
			t.Error("Expected error when setting same bit twice")
		}

		// Test Clear
		if err := bm.Clear(0); err != nil {
			t.Fatalf("Clear(0) failed: %v", err)
		}
		if bm.IsSet(0) {
			t.Error("Expected bit 0 to be clear")
		}
		if bm.Count() != 0 {
			t.Errorf("Expected count 0, got %d", bm.Count())
		}
	})

	t.Run("FindFirstZero", func(t *testing.T) {
		bm := NewBitmap(100)

		// Initially should return 0
		if pos := bm.FindFirstZero(); pos != 0 {
			t.Errorf("Expected first zero at 0, got %d", pos)
		}

		// Set first 5 bits
		for i := 0; i < 5; i++ {
			bm.Set(i)
		}

		// Should return 5
		if pos := bm.FindFirstZero(); pos != 5 {
			t.Errorf("Expected first zero at 5, got %d", pos)
		}
	})

	t.Run("Available count", func(t *testing.T) {
		bm := NewBitmap(100)

		if avail := bm.Available(); avail != 100 {
			t.Errorf("Expected 100 available, got %d", avail)
		}

		bm.Set(0)
		bm.Set(1)

		if avail := bm.Available(); avail != 98 {
			t.Errorf("Expected 98 available, got %d", avail)
		}
	})
}

func TestIPBlock(t *testing.T) {
	t.Run("Create IP block", func(t *testing.T) {
		block, err := NewIPBlock("10.244.1.0/24", "node1")
		if err != nil {
			t.Fatalf("NewIPBlock failed: %v", err)
		}

		if block.Total != 254 {
			t.Errorf("Expected 254 usable IPs, got %d", block.Total)
		}
		if block.NodeID != "node1" {
			t.Errorf("Expected node1, got %s", block.NodeID)
		}
	})

	t.Run("Allocate and release", func(t *testing.T) {
		block, _ := NewIPBlock("10.244.1.0/24", "node1")

		// Allocate first IP
		ip1, err := block.Allocate()
		if err != nil {
			t.Fatalf("Allocate failed: %v", err)
		}

		expectedIP := net.ParseIP("10.244.1.1")
		if !ip1.Equal(expectedIP) {
			t.Errorf("Expected IP %s, got %s", expectedIP, ip1)
		}

		if block.Used != 1 {
			t.Errorf("Expected used count 1, got %d", block.Used)
		}

		// Allocate second IP
		ip2, err := block.Allocate()
		if err != nil {
			t.Fatalf("Second allocate failed: %v", err)
		}

		expectedIP2 := net.ParseIP("10.244.1.2")
		if !ip2.Equal(expectedIP2) {
			t.Errorf("Expected IP %s, got %s", expectedIP2, ip2)
		}

		// Release first IP
		if err := block.Release(ip1); err != nil {
			t.Fatalf("Release failed: %v", err)
		}

		if block.Used != 1 {
			t.Errorf("Expected used count 1 after release, got %d", block.Used)
		}

		// Next allocation should reuse ip1
		ip3, err := block.Allocate()
		if err != nil {
			t.Fatalf("Third allocate failed: %v", err)
		}

		if !ip3.Equal(ip1) {
			t.Errorf("Expected reused IP %s, got %s", ip1, ip3)
		}
	})

	t.Run("Contains check", func(t *testing.T) {
		block, _ := NewIPBlock("10.244.1.0/24", "node1")

		ip, _ := block.Allocate()
		if !block.Contains(ip) {
			t.Error("Expected block to contain allocated IP")
		}

		outsideIP := net.ParseIP("10.244.2.1")
		if block.Contains(outsideIP) {
			t.Error("Expected block to not contain outside IP")
		}
	})

	t.Run("Usage calculation", func(t *testing.T) {
		block, _ := NewIPBlock("10.244.1.0/24", "node1")

		if usage := block.Usage(); usage != 0.0 {
			t.Errorf("Expected 0%% usage, got %.2f%%", usage*100)
		}

		// Allocate half the IPs
		for i := 0; i < 127; i++ {
			block.Allocate()
		}

		usage := block.Usage()
		expectedUsage := 127.0 / 254.0
		if usage < expectedUsage-0.01 || usage > expectedUsage+0.01 {
			t.Errorf("Expected ~50%% usage, got %.2f%%", usage*100)
		}
	})

	t.Run("Exhaust block", func(t *testing.T) {
		// Use a small block for faster test
		block, _ := NewIPBlock("10.244.1.0/29", "node1")
		// /29 has 8 IPs total, 6 usable

		// Allocate all IPs
		for i := 0; i < 6; i++ {
			if _, err := block.Allocate(); err != nil {
				t.Fatalf("Allocate %d failed: %v", i, err)
			}
		}

		// Next allocation should fail
		if _, err := block.Allocate(); err != ErrNoAvailableIP {
			t.Errorf("Expected ErrNoAvailableIP, got %v", err)
		}
	})
}

func BenchmarkBitmapSet(b *testing.B) {
	bm := NewBitmap(10000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pos := i % 10000
		if bm.IsSet(pos) {
			bm.Clear(pos)
		}
		bm.Set(pos)
	}
}

func BenchmarkBitmapFindFirstZero(b *testing.B) {
	bm := NewBitmap(10000)
	// Pre-allocate half
	for i := 0; i < 5000; i++ {
		bm.Set(i * 2)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bm.FindFirstZero()
	}
}

func BenchmarkIPBlockAllocate(b *testing.B) {
	block, _ := NewIPBlock("10.244.1.0/24", "node1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip, err := block.Allocate()
		if err != nil {
			block.Release(ip) // Release to make room
			ip, _ = block.Allocate()
		}
		if i%100 == 0 {
			block.Release(ip) // Release some to keep pool available
		}
	}
}

func BenchmarkIPBlockRelease(b *testing.B) {
	block, _ := NewIPBlock("10.244.1.0/24", "node1")

	// Pre-allocate some IPs
	ips := make([]net.IP, 100)
	for i := 0; i < 100; i++ {
		ips[i], _ = block.Allocate()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := ips[i%100]
		block.Release(ip)
		ips[i%100], _ = block.Allocate()
	}
}
