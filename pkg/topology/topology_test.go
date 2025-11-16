package topology

import (
	"testing"
)

func TestTopology(t *testing.T) {
	t.Run("Add Zone, Pod, TOR, Node", func(t *testing.T) {
		topo := NewTopology()

		// 添加 Zone
		zone := &Zone{
			ID:           "zone-a",
			Name:         "Beijing Zone A",
			SubnetRanges: []string{"10.244.0.0/16"},
		}
		if err := topo.AddZone(zone); err != nil {
			t.Fatalf("Failed to add zone: %v", err)
		}

		// 添加 Pod
		pod := &Pod{
			ID:           "pod-1",
			Name:         "Pod 1",
			ZoneID:       "zone-a",
			SubnetRanges: []string{"10.244.0.0/20"},
		}
		if err := topo.AddPod(pod); err != nil {
			t.Fatalf("Failed to add pod: %v", err)
		}

		// 添加 TOR
		tor := &TOR{
			ID:       "tor-1",
			Name:     "TOR-R01-01",
			PodID:    "pod-1",
			Location: "Rack 01",
			Subnets:  []string{"10.244.0.0/22"},
		}
		if err := topo.AddTOR(tor); err != nil {
			t.Fatalf("Failed to add TOR: %v", err)
		}

		// 注册节点
		node := &Node{
			ID:     "node-1",
			Name:   "k8s-node-1",
			TORID:  "tor-1",
			Labels: map[string]string{"rack": "R01"},
		}
		if err := topo.RegisterNode(node); err != nil {
			t.Fatalf("Failed to register node: %v", err)
		}

		// 验证节点的 TOR
		nodeTOR, err := topo.GetNodeTOR("node-1")
		if err != nil {
			t.Fatalf("Failed to get node TOR: %v", err)
		}
		if nodeTOR.ID != "tor-1" {
			t.Errorf("Expected TOR tor-1, got %s", nodeTOR.ID)
		}
	})

	t.Run("Get topology stats", func(t *testing.T) {
		topo := NewTopology()

		zone := &Zone{ID: "zone-a", Name: "Zone A", SubnetRanges: []string{"10.244.0.0/16"}}
		topo.AddZone(zone)

		pod := &Pod{ID: "pod-1", Name: "Pod 1", ZoneID: "zone-a", SubnetRanges: []string{"10.244.0.0/20"}}
		topo.AddPod(pod)

		tor := &TOR{ID: "tor-1", Name: "TOR 1", PodID: "pod-1", Location: "R01", Subnets: []string{"10.244.0.0/22"}}
		topo.AddTOR(tor)

		node1 := &Node{ID: "node-1", Name: "Node 1", TORID: "tor-1"}
		node2 := &Node{ID: "node-2", Name: "Node 2", TORID: "tor-1"}
		topo.RegisterNode(node1)
		topo.RegisterNode(node2)

		stats := topo.GetTopologyStats()

		if stats.ZoneCount != 1 {
			t.Errorf("Expected 1 zone, got %d", stats.ZoneCount)
		}
		if stats.PodCount != 1 {
			t.Errorf("Expected 1 pod, got %d", stats.PodCount)
		}
		if stats.TORCount != 1 {
			t.Errorf("Expected 1 TOR, got %d", stats.TORCount)
		}
		if stats.NodeCount != 2 {
			t.Errorf("Expected 2 nodes, got %d", stats.NodeCount)
		}
	})

	t.Run("Get node path", func(t *testing.T) {
		topo := NewTopology()

		zone := &Zone{ID: "zone-a", Name: "Beijing Zone A"}
		topo.AddZone(zone)

		pod := &Pod{ID: "pod-1", Name: "Pod 1", ZoneID: "zone-a"}
		topo.AddPod(pod)

		tor := &TOR{ID: "tor-1", Name: "TOR-R01", PodID: "pod-1"}
		topo.AddTOR(tor)

		node := &Node{ID: "node-1", Name: "Node-1", TORID: "tor-1"}
		topo.RegisterNode(node)

		path, err := topo.GetNodePath("node-1")
		if err != nil {
			t.Fatalf("Failed to get node path: %v", err)
		}

		expected := "Beijing Zone A/Pod 1/TOR-R01/Node-1"
		if path != expected {
			t.Errorf("Expected path %s, got %s", expected, path)
		}
	})

	t.Run("Add subnet to TOR", func(t *testing.T) {
		topo := NewTopology()

		zone := &Zone{ID: "zone-a", Name: "Zone A"}
		topo.AddZone(zone)

		pod := &Pod{ID: "pod-1", Name: "Pod 1", ZoneID: "zone-a"}
		topo.AddPod(pod)

		tor := &TOR{ID: "tor-1", Name: "TOR 1", PodID: "pod-1", Subnets: []string{"10.244.0.0/22"}}
		topo.AddTOR(tor)

		// 添加新网段
		if err := topo.AddSubnetToTOR("tor-1", "10.244.4.0/22"); err != nil {
			t.Fatalf("Failed to add subnet to TOR: %v", err)
		}

		subnets, err := topo.GetTORSubnets("tor-1")
		if err != nil {
			t.Fatalf("Failed to get TOR subnets: %v", err)
		}

		if len(subnets) != 2 {
			t.Errorf("Expected 2 subnets, got %d", len(subnets))
		}
	})
}
