package topology

import (
	"net"
	"testing"
)

func TestSubnetPool(t *testing.T) {
	t.Run("Add subnet to pool", func(t *testing.T) {
		pool := NewSubnetPool("tor-1")

		err := pool.AddSubnet("10.244.0.0/22", "default")
		if err != nil {
			t.Fatalf("Failed to add subnet: %v", err)
		}

		stats := pool.GetStats()
		if stats.SubnetCount != 1 {
			t.Errorf("Expected 1 subnet, got %d", stats.SubnetCount)
		}

		// /22 = 1024 IPs, usable = 1022
		expectedCapacity := 1022
		if stats.TotalCapacity != expectedCapacity {
			t.Errorf("Expected capacity %d, got %d", expectedCapacity, stats.TotalCapacity)
		}
	})

	t.Run("Allocate and release IP", func(t *testing.T) {
		pool := NewSubnetPool("tor-1")
		pool.AddSubnet("10.244.0.0/24", "default")

		// 分配 IP
		ip, cidr, err := pool.AllocateIP("node-1", "default")
		if err != nil {
			t.Fatalf("Failed to allocate IP: %v", err)
		}

		if ip == nil {
			t.Fatal("Expected non-nil IP")
		}

		if cidr != "10.244.0.0/24" {
			t.Errorf("Expected CIDR 10.244.0.0/24, got %s", cidr)
		}

		// 应该是第一个可用 IP (10.244.0.1)
		expectedIP := net.ParseIP("10.244.0.1")
		if !ip.Equal(expectedIP) {
			t.Errorf("Expected IP %s, got %s", expectedIP, ip)
		}

		// 检查使用量
		stats := pool.GetStats()
		if stats.TotalUsed != 1 {
			t.Errorf("Expected 1 used IP, got %d", stats.TotalUsed)
		}

		// 释放 IP
		if err := pool.ReleaseIP(ip); err != nil {
			t.Fatalf("Failed to release IP: %v", err)
		}

		stats = pool.GetStats()
		if stats.TotalUsed != 0 {
			t.Errorf("Expected 0 used IPs after release, got %d", stats.TotalUsed)
		}
	})

	t.Run("Allocate from multiple subnets", func(t *testing.T) {
		pool := NewSubnetPool("tor-1")
		pool.AddSubnet("10.244.0.0/24", "default")
		pool.AddSubnet("10.244.100.0/24", "storage")

		// 分配默认用途的 IP
		ip1, cidr1, err := pool.AllocateIP("node-1", "default")
		if err != nil {
			t.Fatalf("Failed to allocate default IP: %v", err)
		}

		// 应该从默认网段分配
		_, defaultNet, _ := net.ParseCIDR("10.244.0.0/24")
		if !defaultNet.Contains(ip1) {
			t.Errorf("IP %s not in default subnet", ip1)
		}
		if cidr1 != "10.244.0.0/24" {
			t.Errorf("Expected CIDR 10.244.0.0/24, got %s", cidr1)
		}

		// 分配存储用途的 IP
		ip2, cidr2, err := pool.AllocateIP("node-1", "storage")
		if err != nil {
			t.Fatalf("Failed to allocate storage IP: %v", err)
		}

		// 应该从存储网段分配
		_, storageNet, _ := net.ParseCIDR("10.244.100.0/24")
		if !storageNet.Contains(ip2) {
			t.Errorf("IP %s not in storage subnet", ip2)
		}
		if cidr2 != "10.244.100.0/24" {
			t.Errorf("Expected CIDR 10.244.100.0/24, got %s", cidr2)
		}

		// 检查每个网段的使用量
		stats := pool.GetStats()
		if stats.SubnetStats["10.244.0.0/24"].Used != 1 {
			t.Error("Expected 1 IP used in default subnet")
		}
		if stats.SubnetStats["10.244.100.0/24"].Used != 1 {
			t.Error("Expected 1 IP used in storage subnet")
		}
	})

	t.Run("Get allocation info", func(t *testing.T) {
		pool := NewSubnetPool("tor-1")
		pool.AddSubnet("10.244.0.0/24", "default")

		ip, _, _ := pool.AllocateIP("node-1", "default")

		allocation, err := pool.GetAllocation(ip)
		if err != nil {
			t.Fatalf("Failed to get allocation: %v", err)
		}

		if allocation.NodeID != "node-1" {
			t.Errorf("Expected node-1, got %s", allocation.NodeID)
		}

		if allocation.IP != ip.String() {
			t.Errorf("Expected IP %s, got %s", ip.String(), allocation.IP)
		}
	})

	t.Run("List all allocations", func(t *testing.T) {
		pool := NewSubnetPool("tor-1")
		pool.AddSubnet("10.244.0.0/24", "default")

		// 分配 5 个 IP
		for i := 0; i < 5; i++ {
			pool.AllocateIP("node-1", "default")
		}

		allocations := pool.ListAllocations()
		if len(allocations) != 5 {
			t.Errorf("Expected 5 allocations, got %d", len(allocations))
		}
	})

	t.Run("Subnet exhaustion", func(t *testing.T) {
		// 使用小网段测试耗尽
		pool := NewSubnetPool("tor-1")
		pool.AddSubnet("10.244.0.0/30", "default") // 只有 2 个可用 IP

		// 分配第一个
		ip1, _, err := pool.AllocateIP("node-1", "default")
		if err != nil {
			t.Fatalf("First allocation failed: %v", err)
		}

		// 分配第二个
		ip2, _, err := pool.AllocateIP("node-1", "default")
		if err != nil {
			t.Fatalf("Second allocation failed: %v", err)
		}

		// 应该不同
		if ip1.Equal(ip2) {
			t.Error("Expected different IPs")
		}

		// 第三个应该失败
		_, _, err = pool.AllocateIP("node-1", "default")
		if err == nil {
			t.Error("Expected allocation to fail when subnet exhausted")
		}

		// 释放一个后应该可以再分配
		pool.ReleaseIP(ip1)

		ip3, _, err := pool.AllocateIP("node-1", "default")
		if err != nil {
			t.Fatalf("Allocation after release failed: %v", err)
		}

		if !ip3.Equal(ip1) {
			t.Error("Expected to reuse released IP")
		}
	})

	t.Run("Usage stats", func(t *testing.T) {
		pool := NewSubnetPool("tor-1")
		pool.AddSubnet("10.244.0.0/24", "default")

		// 分配一半的 IP (254 / 2 = 127)
		for i := 0; i < 127; i++ {
			pool.AllocateIP("node-1", "default")
		}

		stats := pool.GetStats()
		if stats.TotalUsed != 127 {
			t.Errorf("Expected 127 used IPs, got %d", stats.TotalUsed)
		}

		// 使用率应该约 50%
		if stats.UsageRate < 0.49 || stats.UsageRate > 0.51 {
			t.Errorf("Expected ~50%% usage rate, got %.2f%%", stats.UsageRate*100)
		}
	})
}
