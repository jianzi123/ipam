package raft

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"github.com/jianzi123/ipam/pkg/ipam"
)

// TopologyNode represents a Raft node with topology-aware IPAM
type TopologyNode struct {
	config *NodeConfig
	raft   *raft.Raft
	fsm    *TopologyFSM
	pool   *ipam.TopologyAwarePool
}

// NewTopologyNode creates a new topology-aware Raft node
func NewTopologyNode(config *NodeConfig, pool *ipam.TopologyAwarePool) (*TopologyNode, error) {
	// Create FSM
	fsm := NewTopologyFSM(pool)

	// Setup Raft configuration
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(config.NodeID)

	if config.HeartbeatTimeout > 0 {
		raftConfig.HeartbeatTimeout = config.HeartbeatTimeout
	}
	if config.ElectionTimeout > 0 {
		raftConfig.ElectionTimeout = config.ElectionTimeout
	}
	if config.CommitTimeout > 0 {
		raftConfig.CommitTimeout = config.CommitTimeout
	}

	// Create data directory
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Setup log store
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-log.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create log store: %w", err)
	}

	// Setup stable store
	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-stable.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create stable store: %w", err)
	}

	// Create snapshot store
	snapshotStore, err := raft.NewFileSnapshotStore(config.DataDir, 3, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Setup transport
	addr, err := net.ResolveTCPAddr("tcp", config.BindAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve bind address: %w", err)
	}

	transport, err := raft.NewTCPTransport(config.BindAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// Create Raft instance
	r, err := raft.NewRaft(raftConfig, fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft: %w", err)
	}

	node := &TopologyNode{
		config: config,
		raft:   r,
		fsm:    fsm,
		pool:   pool,
	}

	// Bootstrap cluster if needed
	if config.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(config.NodeID),
					Address: raft.ServerAddress(config.BindAddr),
				},
			},
		}
		if err := r.BootstrapCluster(configuration).Error(); err != nil {
			return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
		}
	}

	return node, nil
}

// IsLeader checks if this node is the leader
func (n *TopologyNode) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

// Leader returns the current leader address
func (n *TopologyNode) Leader() string {
	addr, _ := n.raft.LeaderWithID()
	return string(addr)
}

// InitializeTopology initializes the network topology
// This goes through Raft consensus
func (n *TopologyNode) InitializeTopology(config *ipam.TopologyConfig) (map[string]interface{}, error) {
	data := InitTopologyData{
		Config: config,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	cmd := TopologyCommand{
		Type: CommandInitTopology,
		Data: dataBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return nil, fmt.Errorf("raft apply failed: %w", err)
	}

	response := future.Response().(*FSMResponse)
	if !response.Success {
		return nil, fmt.Errorf("command failed: %s", response.Error)
	}

	return response.Data, nil
}

// RegisterNode registers a node to the topology
// This goes through Raft consensus
func (n *TopologyNode) RegisterNode(nodeID, nodeName, torID string, labels map[string]string) (map[string]interface{}, error) {
	data := RegisterNodeData{
		NodeID:   nodeID,
		NodeName: nodeName,
		TORID:    torID,
		Labels:   labels,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	cmd := TopologyCommand{
		Type: CommandRegisterNode,
		Data: dataBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return nil, fmt.Errorf("raft apply failed: %w", err)
	}

	response := future.Response().(*FSMResponse)
	if !response.Success {
		return nil, fmt.Errorf("command failed: %s", response.Error)
	}

	return response.Data, nil
}

// AllocateIP allocates an IP for a node
// This goes through Raft consensus
func (n *TopologyNode) AllocateIP(nodeID, purpose string) (map[string]interface{}, error) {
	data := AllocateIPData{
		NodeID:  nodeID,
		Purpose: purpose,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	cmd := TopologyCommand{
		Type: CommandAllocateIP,
		Data: dataBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return nil, fmt.Errorf("raft apply failed: %w", err)
	}

	response := future.Response().(*FSMResponse)
	if !response.Success {
		return nil, fmt.Errorf("command failed: %s", response.Error)
	}

	return response.Data, nil
}

// ReleaseIP releases an IP from a node
// This goes through Raft consensus
func (n *TopologyNode) ReleaseIP(nodeID, ip string) error {
	data := ReleaseIPData{
		NodeID: nodeID,
		IP:     ip,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	cmd := TopologyCommand{
		Type: CommandReleaseIP,
		Data: dataBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("raft apply failed: %w", err)
	}

	response := future.Response().(*FSMResponse)
	if !response.Success {
		return fmt.Errorf("command failed: %s", response.Error)
	}

	return nil
}

// AddSubnetToTOR adds a subnet to a TOR
// This goes through Raft consensus
func (n *TopologyNode) AddSubnetToTOR(torID, cidr, purpose string) (map[string]interface{}, error) {
	data := AddSubnetData{
		TORID:   torID,
		CIDR:    cidr,
		Purpose: purpose,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	cmd := TopologyCommand{
		Type: CommandAddSubnet,
		Data: dataBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return nil, fmt.Errorf("raft apply failed: %w", err)
	}

	response := future.Response().(*FSMResponse)
	if !response.Success {
		return nil, fmt.Errorf("command failed: %s", response.Error)
	}

	return response.Data, nil
}

// Join adds a new node to the Raft cluster
func (n *TopologyNode) Join(nodeID, addr string) error {
	if !n.IsLeader() {
		return fmt.Errorf("not the leader")
	}

	future := n.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 0)
	return future.Error()
}

// Leave removes a node from the Raft cluster
func (n *TopologyNode) Leave(nodeID string) error {
	if !n.IsLeader() {
		return fmt.Errorf("not the leader")
	}

	future := n.raft.RemoveServer(raft.ServerID(nodeID), 0, 0)
	return future.Error()
}

// Stats returns Raft statistics
func (n *TopologyNode) Stats() map[string]string {
	return n.raft.Stats()
}

// Shutdown gracefully shuts down the Raft node
func (n *TopologyNode) Shutdown() error {
	future := n.raft.Shutdown()
	return future.Error()
}

// GetPool returns the underlying topology-aware IP pool
// This allows read-only access to pool state
func (n *TopologyNode) GetPool() *ipam.TopologyAwarePool {
	return n.pool
}
