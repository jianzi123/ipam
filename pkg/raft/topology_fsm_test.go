package raft

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/raft"
	"github.com/jianzi123/ipam/pkg/ipam"
)

func TestTopologyFSM(t *testing.T) {
	t.Run("Initialize topology", func(t *testing.T) {
		pool := ipam.NewTopologyAwarePool("10.244.0.0/16")
		fsm := NewTopologyFSM(pool)

		config := &ipam.TopologyConfig{
			Zones: []ipam.ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []ipam.PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []ipam.TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []ipam.SubnetConfig{
										{CIDR: "10.244.0.0/24", Purpose: "default"},
									},
								},
							},
						},
					},
				},
			},
		}

		data := InitTopologyData{Config: config}
		dataBytes, _ := json.Marshal(data)

		cmd := TopologyCommand{
			Type: CommandInitTopology,
			Data: dataBytes,
		}
		cmdBytes, _ := json.Marshal(cmd)

		log := &raft.Log{
			Data: cmdBytes,
		}

		result := fsm.Apply(log)
		response := result.(*FSMResponse)

		if !response.Success {
			t.Fatalf("Failed to initialize topology: %s", response.Error)
		}

		if response.Data["zones"] != 1 {
			t.Errorf("Expected 1 zone, got %v", response.Data["zones"])
		}
	})

	t.Run("Register node", func(t *testing.T) {
		pool := ipam.NewTopologyAwarePool("10.244.0.0/16")
		fsm := NewTopologyFSM(pool)

		// First initialize topology
		config := &ipam.TopologyConfig{
			Zones: []ipam.ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []ipam.PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []ipam.TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []ipam.SubnetConfig{
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

		// Register node
		nodeData := RegisterNodeData{
			NodeID:   "node-1",
			NodeName: "k8s-node-1",
			TORID:    "tor-1",
			Labels:   map[string]string{"rack": "R01"},
		}
		dataBytes, _ := json.Marshal(nodeData)

		cmd := TopologyCommand{
			Type: CommandRegisterNode,
			Data: dataBytes,
		}
		cmdBytes, _ := json.Marshal(cmd)

		log := &raft.Log{
			Data: cmdBytes,
		}

		result := fsm.Apply(log)
		response := result.(*FSMResponse)

		if !response.Success {
			t.Fatalf("Failed to register node: %s", response.Error)
		}

		if response.Data["node_id"] != "node-1" {
			t.Errorf("Expected node-1, got %v", response.Data["node_id"])
		}
	})

	t.Run("Allocate IP", func(t *testing.T) {
		pool := ipam.NewTopologyAwarePool("10.244.0.0/16")
		fsm := NewTopologyFSM(pool)

		// Initialize topology
		config := &ipam.TopologyConfig{
			Zones: []ipam.ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []ipam.PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []ipam.TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []ipam.SubnetConfig{
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
		pool.RegisterNode("node-1", "k8s-node-1", "tor-1", nil)

		// Allocate IP
		allocData := AllocateIPData{
			NodeID:  "node-1",
			Purpose: "default",
		}
		dataBytes, _ := json.Marshal(allocData)

		cmd := TopologyCommand{
			Type: CommandAllocateIP,
			Data: dataBytes,
		}
		cmdBytes, _ := json.Marshal(cmd)

		log := &raft.Log{
			Data: cmdBytes,
		}

		result := fsm.Apply(log)
		response := result.(*FSMResponse)

		if !response.Success {
			t.Fatalf("Failed to allocate IP: %s", response.Error)
		}

		if response.Data["ip"] == nil {
			t.Error("Expected IP address in response")
		}

		if response.Data["cidr"] != "10.244.0.0/24" {
			t.Errorf("Expected CIDR 10.244.0.0/24, got %v", response.Data["cidr"])
		}
	})

	t.Run("Release IP", func(t *testing.T) {
		pool := ipam.NewTopologyAwarePool("10.244.0.0/16")
		fsm := NewTopologyFSM(pool)

		// Initialize and allocate
		config := &ipam.TopologyConfig{
			Zones: []ipam.ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []ipam.PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []ipam.TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []ipam.SubnetConfig{
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
		pool.RegisterNode("node-1", "k8s-node-1", "tor-1", nil)

		ip, _, _ := pool.AllocateIPForNode("node-1")

		// Release IP
		releaseData := ReleaseIPData{
			NodeID: "node-1",
			IP:     ip.String(),
		}
		dataBytes, _ := json.Marshal(releaseData)

		cmd := TopologyCommand{
			Type: CommandReleaseIP,
			Data: dataBytes,
		}
		cmdBytes, _ := json.Marshal(cmd)

		log := &raft.Log{
			Data: cmdBytes,
		}

		result := fsm.Apply(log)
		response := result.(*FSMResponse)

		if !response.Success {
			t.Fatalf("Failed to release IP: %s", response.Error)
		}

		// Verify usage decreased
		stats := pool.GetPoolStats()
		if stats.TotalUsed != 0 {
			t.Errorf("Expected 0 used IPs, got %d", stats.TotalUsed)
		}
	})

	t.Run("Add subnet to TOR", func(t *testing.T) {
		pool := ipam.NewTopologyAwarePool("10.244.0.0/16")
		fsm := NewTopologyFSM(pool)

		// Initialize topology
		config := &ipam.TopologyConfig{
			Zones: []ipam.ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []ipam.PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []ipam.TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []ipam.SubnetConfig{
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

		// Add subnet
		subnetData := AddSubnetData{
			TORID:   "tor-1",
			CIDR:    "10.244.4.0/24",
			Purpose: "storage",
		}
		dataBytes, _ := json.Marshal(subnetData)

		cmd := TopologyCommand{
			Type: CommandAddSubnet,
			Data: dataBytes,
		}
		cmdBytes, _ := json.Marshal(cmd)

		log := &raft.Log{
			Data: cmdBytes,
		}

		result := fsm.Apply(log)
		response := result.(*FSMResponse)

		if !response.Success {
			t.Fatalf("Failed to add subnet: %s", response.Error)
		}

		// Verify subnet count increased
		stats := pool.GetPoolStats()
		if stats.TotalSubnets != 2 {
			t.Errorf("Expected 2 subnets, got %d", stats.TotalSubnets)
		}
	})

	t.Run("Unknown command type", func(t *testing.T) {
		pool := ipam.NewTopologyAwarePool("10.244.0.0/16")
		fsm := NewTopologyFSM(pool)

		cmd := TopologyCommand{
			Type: TopologyCommandType("unknown"),
			Data: json.RawMessage(`{}`),
		}
		cmdBytes, _ := json.Marshal(cmd)

		log := &raft.Log{
			Data: cmdBytes,
		}

		result := fsm.Apply(log)
		response := result.(*FSMResponse)

		if response.Success {
			t.Error("Expected command to fail with unknown type")
		}
	})

	t.Run("Snapshot and restore", func(t *testing.T) {
		pool := ipam.NewTopologyAwarePool("10.244.0.0/16")
		fsm := NewTopologyFSM(pool)

		// Initialize topology
		config := &ipam.TopologyConfig{
			Zones: []ipam.ZoneConfig{
				{
					ID:   "zone-a",
					Name: "Zone A",
					Pods: []ipam.PodConfig{
						{
							ID:   "pod-1",
							Name: "Pod 1",
							TORs: []ipam.TORConfig{
								{
									ID:       "tor-1",
									Name:     "TOR 1",
									Location: "R01",
									Subnets: []ipam.SubnetConfig{
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

		// Take snapshot
		snapshot, err := fsm.Snapshot()
		if err != nil {
			t.Fatalf("Failed to create snapshot: %v", err)
		}

		if snapshot == nil {
			t.Fatal("Expected non-nil snapshot")
		}

		// Verify snapshot type
		if _, ok := snapshot.(*TopologyFSMSnapshot); !ok {
			t.Error("Expected TopologyFSMSnapshot type")
		}
	})
}
