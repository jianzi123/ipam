package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/hashicorp/raft"
	"github.com/jianzi123/ipam/pkg/ipam"
)

// TopologyFSM implements the Raft Finite State Machine for topology-aware IPAM
// It manages the replicated state of topology and IP allocations
type TopologyFSM struct {
	pool *ipam.TopologyAwarePool
	mu   sync.RWMutex
}

// TopologyCommandType represents the type of topology Raft command
type TopologyCommandType string

const (
	CommandInitTopology  TopologyCommandType = "init_topology"
	CommandRegisterNode  TopologyCommandType = "register_node"
	CommandAllocateIP    TopologyCommandType = "allocate_ip"
	CommandReleaseIP     TopologyCommandType = "release_ip"
	CommandAddSubnet     TopologyCommandType = "add_subnet"
)

// TopologyCommand represents a Raft log command for topology operations
type TopologyCommand struct {
	Type TopologyCommandType `json:"type"`
	Data json.RawMessage     `json:"data"`
}

// InitTopologyData contains data for topology initialization
type InitTopologyData struct {
	Config *ipam.TopologyConfig `json:"config"`
}

// RegisterNodeData contains data for node registration
type RegisterNodeData struct {
	NodeID   string            `json:"node_id"`
	NodeName string            `json:"node_name"`
	TORID    string            `json:"tor_id"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// AllocateIPData contains data for IP allocation
type AllocateIPData struct {
	NodeID  string `json:"node_id"`
	Purpose string `json:"purpose"`
}

// ReleaseIPData contains data for IP release
type ReleaseIPData struct {
	NodeID string `json:"node_id"`
	IP     string `json:"ip"`
}

// AddSubnetData contains data for adding subnet to TOR
type AddSubnetData struct {
	TORID   string `json:"tor_id"`
	CIDR    string `json:"cidr"`
	Purpose string `json:"purpose"`
}

// NewTopologyFSM creates a new topology-aware IPAM FSM
func NewTopologyFSM(pool *ipam.TopologyAwarePool) *TopologyFSM {
	return &TopologyFSM{
		pool: pool,
	}
}

// Apply applies a Raft log entry to the FSM
// This is called by Raft when a command is committed
func (f *TopologyFSM) Apply(log *raft.Log) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	var cmd TopologyCommand
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal command: %v", err)}
	}

	switch cmd.Type {
	case CommandInitTopology:
		return f.applyInitTopology(cmd)
	case CommandRegisterNode:
		return f.applyRegisterNode(cmd)
	case CommandAllocateIP:
		return f.applyAllocateIP(cmd)
	case CommandReleaseIP:
		return f.applyReleaseIP(cmd)
	case CommandAddSubnet:
		return f.applyAddSubnet(cmd)
	default:
		return &FSMResponse{Success: false, Error: fmt.Sprintf("unknown command type: %s", cmd.Type)}
	}
}

// applyInitTopology initializes the network topology
func (f *TopologyFSM) applyInitTopology(cmd TopologyCommand) interface{} {
	var data InitTopologyData
	if err := json.Unmarshal(cmd.Data, &data); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal data: %v", err)}
	}

	if err := f.pool.InitializeTopology(data.Config); err != nil {
		return &FSMResponse{Success: false, Error: err.Error()}
	}

	stats := f.pool.GetPoolStats()
	return &FSMResponse{
		Success: true,
		Data: map[string]interface{}{
			"zones":  stats.ZoneCount,
			"pods":   stats.PodCount,
			"tors":   stats.TORCount,
			"subnets": stats.TotalSubnets,
		},
	}
}

// applyRegisterNode registers a node to the topology
func (f *TopologyFSM) applyRegisterNode(cmd TopologyCommand) interface{} {
	var data RegisterNodeData
	if err := json.Unmarshal(cmd.Data, &data); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal data: %v", err)}
	}

	if err := f.pool.RegisterNode(data.NodeID, data.NodeName, data.TORID, data.Labels); err != nil {
		return &FSMResponse{Success: false, Error: err.Error()}
	}

	return &FSMResponse{
		Success: true,
		Data: map[string]interface{}{
			"node_id":   data.NodeID,
			"node_name": data.NodeName,
			"tor_id":    data.TORID,
		},
	}
}

// applyAllocateIP allocates an IP for a node
func (f *TopologyFSM) applyAllocateIP(cmd TopologyCommand) interface{} {
	var data AllocateIPData
	if err := json.Unmarshal(cmd.Data, &data); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal data: %v", err)}
	}

	purpose := data.Purpose
	if purpose == "" {
		purpose = "default"
	}

	ip, cidr, err := f.pool.AllocateIPForNodeWithPurpose(data.NodeID, purpose)
	if err != nil {
		return &FSMResponse{Success: false, Error: err.Error()}
	}

	return &FSMResponse{
		Success: true,
		Data: map[string]interface{}{
			"ip":      ip.String(),
			"cidr":    cidr,
			"node_id": data.NodeID,
			"purpose": purpose,
		},
	}
}

// applyReleaseIP releases an IP from a node
func (f *TopologyFSM) applyReleaseIP(cmd TopologyCommand) interface{} {
	var data ReleaseIPData
	if err := json.Unmarshal(cmd.Data, &data); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal data: %v", err)}
	}

	ip := net.ParseIP(data.IP)
	if ip == nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("invalid IP address: %s", data.IP)}
	}

	if err := f.pool.ReleaseIPForNode(data.NodeID, ip); err != nil {
		return &FSMResponse{Success: false, Error: err.Error()}
	}

	return &FSMResponse{
		Success: true,
		Data: map[string]interface{}{
			"ip":      data.IP,
			"node_id": data.NodeID,
		},
	}
}

// applyAddSubnet adds a subnet to a TOR
func (f *TopologyFSM) applyAddSubnet(cmd TopologyCommand) interface{} {
	var data AddSubnetData
	if err := json.Unmarshal(cmd.Data, &data); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal data: %v", err)}
	}

	if err := f.pool.AddSubnetToTOR(data.TORID, data.CIDR, data.Purpose); err != nil {
		return &FSMResponse{Success: false, Error: err.Error()}
	}

	return &FSMResponse{
		Success: true,
		Data: map[string]interface{}{
			"tor_id":  data.TORID,
			"cidr":    data.CIDR,
			"purpose": data.Purpose,
		},
	}
}

// Snapshot returns a snapshot of the FSM state
// This is used for log compaction
func (f *TopologyFSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Get current pool stats to snapshot
	stats := f.pool.GetPoolStats()

	return &TopologyFSMSnapshot{
		stats: stats,
	}, nil
}

// Restore restores the FSM state from a snapshot
func (f *TopologyFSM) Restore(snapshot io.ReadCloser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	defer snapshot.Close()

	// Read snapshot data
	var snapshotData TopologySnapshotData
	decoder := json.NewDecoder(snapshot)
	if err := decoder.Decode(&snapshotData); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Restore topology configuration
	if snapshotData.TopologyConfig != nil {
		if err := f.pool.InitializeTopology(snapshotData.TopologyConfig); err != nil {
			return fmt.Errorf("failed to restore topology: %w", err)
		}
	}

	// Restore node registrations
	for _, nodeData := range snapshotData.Nodes {
		if err := f.pool.RegisterNode(nodeData.NodeID, nodeData.NodeName, nodeData.TORID, nodeData.Labels); err != nil {
			return fmt.Errorf("failed to restore node %s: %w", nodeData.NodeID, err)
		}
	}

	// Note: IP allocations are restored through the subnet pools
	// which are part of the topology initialization

	return nil
}

// TopologyFSMSnapshot represents a point-in-time snapshot of the topology FSM
type TopologyFSMSnapshot struct {
	stats *ipam.TopologyPoolStats
}

// Persist writes the snapshot to the given sink
func (s *TopologyFSMSnapshot) Persist(sink raft.SnapshotSink) error {
	// For now, we only persist stats
	// In production, you'd want to persist full topology config and allocations
	data := TopologySnapshotData{
		Stats: s.stats,
	}

	// Encode as JSON
	encoder := json.NewEncoder(sink)
	if err := encoder.Encode(&data); err != nil {
		sink.Cancel()
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	return sink.Close()
}

// Release is called when the snapshot is no longer needed
func (s *TopologyFSMSnapshot) Release() {
	// Nothing to release in this simple implementation
}

// TopologySnapshotData represents the data stored in a topology snapshot
type TopologySnapshotData struct {
	Stats          *ipam.TopologyPoolStats `json:"stats"`
	TopologyConfig *ipam.TopologyConfig    `json:"topology_config,omitempty"`
	Nodes          []RegisterNodeData      `json:"nodes,omitempty"`
}
