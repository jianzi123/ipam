package ipam

import (
	"encoding/json"
	"testing"
)

func TestTopologyAwarePool(t *testing.T) {
	t.Run("Initialize topology from config", func(t *testing.T) {
		pool := NewTopologyAwarePool("10.244.0.0/16")

		config := &TopologyConfig{
			Zones: []ZoneConfig{
				{
					ID:           "zone-a",
					Name:         "Beijing Zone A",
					SubnetRanges: []string{"10.244.0.0/16"},
					Pods: []PodConfig{
						{
							ID:           "pod-1",
							Name:         "Pod 1",
							SubnetRanges: []string{"10.244.0.0/20"},
							TORs: []TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR-R01",
									Location: "Rack 01",
									Subnets: []SubnetConfig{
										{CIDR: "10.244.0.0/22", Purpose: "default"},
										{CIDR: "10.244.100.0/24", Purpose: "storage"},
									},
								},
							},
						},
					},
				},
			},
		}

		if err := pool.InitializeTopology(config); err != nil {
			t.Fatalf("Failed to initialize topology: %v", err)
		}

		stats := pool.GetPoolStats()
		if stats.ZoneCount != 1 {
			t.Errorf("Expected 1 zone, got %d", stats.ZoneCount)
		}
		if stats.TORCount != 1 {
			t.Errorf("Expected 1 TOR, got %d", stats.TORCount)
		}
		if stats.TotalSubnets != 2 {
			t.Errorf("Expected 2 subnets, got %d", stats.TotalSubnets)
		}
	})

	t.Run("Register node and allocate IP", func(t *testing.T) {
		pool := NewTopologyAwarePool("10.244.0.0/16")

		config := &TopologyConfig{
			Zones: []ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []SubnetConfig{
										{CIDR: "10.244.0.0/24", Purpose: "default"},
									},
								},
							},
						},
					},
				},
			},
		}

		pool.InitializeTopology(config)

		// 注册节点
		err := pool.RegisterNode("node-1", "k8s-node-1", "tor-1", map[string]string{"rack": "R01"})
		if err != nil {
			t.Fatalf("Failed to register node: %v", err)
		}

		// 分配 IP
		ip, cidr, err := pool.AllocateIPForNode("node-1")
		if err != nil {
			t.Fatalf("Failed to allocate IP: %v", err)
		}

		if ip == nil {
			t.Fatal("Expected non-nil IP")
		}

		if cidr != "10.244.0.0/24" {
			t.Errorf("Expected CIDR 10.244.0.0/24, got %s", cidr)
		}

		// 验证统计
		stats := pool.GetPoolStats()
		if stats.TotalUsed != 1 {
			t.Errorf("Expected 1 used IP, got %d", stats.TotalUsed)
		}
	})

	t.Run("Multi-purpose subnets", func(t *testing.T) {
		pool := NewTopologyAwarePool("10.244.0.0/16")

		config := &TopologyConfig{
			Zones: []ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []SubnetConfig{
										{CIDR: "10.244.0.0/24", Purpose: "default"},
										{CIDR: "10.244.100.0/24", Purpose: "storage"},
									},
								},
							},
						},
					},
				},
			},
		}

		pool.InitializeTopology(config)
		pool.RegisterNode("node-1", "node-1", "tor-1", nil)

		// 分配默认用途的 IP
		ip1, cidr1, _ := pool.AllocateIPForNodeWithPurpose("node-1", "default")
		if cidr1 != "10.244.0.0/24" {
			t.Errorf("Expected default subnet, got %s", cidr1)
		}

		// 分配存储用途的 IP
		ip2, cidr2, _ := pool.AllocateIPForNodeWithPurpose("node-1", "storage")
		if cidr2 != "10.244.100.0/24" {
			t.Errorf("Expected storage subnet, got %s", cidr2)
		}

		// IPs 应该来自不同网段
		if ip1.Equal(ip2) {
			t.Error("Expected different IPs from different subnets")
		}
	})

	t.Run("Release IP", func(t *testing.T) {
		pool := NewTopologyAwarePool("10.244.0.0/16")

		config := &TopologyConfig{
			Zones: []ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []SubnetConfig{
										{CIDR: "10.244.0.0/24", Purpose: "default"},
									},
								},
							},
						},
					},
				},
			},
		}

		pool.InitializeTopology(config)
		pool.RegisterNode("node-1", "node-1", "tor-1", nil)

		ip, _, _ := pool.AllocateIPForNode("node-1")

		// 释放 IP
		err := pool.ReleaseIPForNode("node-1", ip)
		if err != nil {
			t.Fatalf("Failed to release IP: %v", err)
		}

		// 验证统计
		stats := pool.GetPoolStats()
		if stats.TotalUsed != 0 {
			t.Errorf("Expected 0 used IPs after release, got %d", stats.TotalUsed)
		}
	})

	t.Run("Get node stats", func(t *testing.T) {
		pool := NewTopologyAwarePool("10.244.0.0/16")

		config := &TopologyConfig{
			Zones: []ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []SubnetConfig{
										{CIDR: "10.244.0.0/24", Purpose: "default"},
									},
								},
							},
						},
					},
				},
			},
		}

		pool.InitializeTopology(config)
		pool.RegisterNode("node-1", "node-1", "tor-1", nil)

		// 分配几个 IP
		for i := 0; i < 5; i++ {
			pool.AllocateIPForNode("node-1")
		}

		// 获取节点统计
		nodeStats, err := pool.GetNodeStats("node-1")
		if err != nil {
			t.Fatalf("Failed to get node stats: %v", err)
		}

		if nodeStats.AllocatedIPs != 5 {
			t.Errorf("Expected 5 allocated IPs, got %d", nodeStats.AllocatedIPs)
		}

		if nodeStats.TORID != "tor-1" {
			t.Errorf("Expected TOR tor-1, got %s", nodeStats.TORID)
		}
	})

	t.Run("Add subnet to TOR dynamically", func(t *testing.T) {
		pool := NewTopologyAwarePool("10.244.0.0/16")

		config := &TopologyConfig{
			Zones: []ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []SubnetConfig{
										{CIDR: "10.244.0.0/24", Purpose: "default"},
									},
								},
							},
						},
					},
				},
			},
		}

		pool.InitializeTopology(config)

		// 动态添加新网段
		err := pool.AddSubnetToTOR("tor-1", "10.244.4.0/24", "default")
		if err != nil {
			t.Fatalf("Failed to add subnet: %v", err)
		}

		// 验证统计
		stats := pool.GetPoolStats()
		if stats.TotalSubnets != 2 {
			t.Errorf("Expected 2 subnets after adding, got %d", stats.TotalSubnets)
		}
	})
}

func TestTopologyConfigJSON(t *testing.T) {
	t.Run("Parse topology config from JSON", func(t *testing.T) {
		jsonConfig := `{
			"zones": [
				{
					"id": "zone-a",
					"name": "Beijing Zone A",
					"subnet_ranges": ["10.244.0.0/16"],
					"pods": [
						{
							"id": "pod-1",
							"name": "Pod 1",
							"subnet_ranges": ["10.244.0.0/20"],
							"tors": [
								{
									"id": "tor-1",
									"name": "TOR-R01",
									"location": "Rack 01",
									"subnets": [
										{"cidr": "10.244.0.0/22", "purpose": "default"},
										{"cidr": "10.244.100.0/24", "purpose": "storage"}
									]
								}
							]
						}
					]
				}
			]
		}`

		var config TopologyConfig
		if err := json.Unmarshal([]byte(jsonConfig), &config); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}

		pool := NewTopologyAwarePool("10.244.0.0/16")
		if err := pool.InitializeTopology(&config); err != nil {
			t.Fatalf("Failed to initialize topology: %v", err)
		}

		stats := pool.GetPoolStats()
		if stats.ZoneCount != 1 {
			t.Errorf("Expected 1 zone, got %d", stats.ZoneCount)
		}
	})
}
