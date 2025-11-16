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

// NodeConfig contains configuration for a Raft node
type NodeConfig struct {
	NodeID          string        // Unique node identifier
	BindAddr        string        // Address to bind Raft (e.g., "0.0.0.0:7000")
	DataDir         string        // Directory for Raft data
	Bootstrap       bool          // Bootstrap a new cluster
	JoinAddr        string        // Address of existing node to join
	HeartbeatTimeout time.Duration // Heartbeat timeout
	ElectionTimeout  time.Duration // Election timeout
	CommitTimeout    time.Duration // Commit timeout
}

// Node represents a Raft node
type Node struct {
	config *NodeConfig
	raft   *raft.Raft
	fsm    *FSM
	pool   *ipam.Pool
}

// NewNode creates a new Raft node
func NewNode(config *NodeConfig, pool *ipam.Pool) (*Node, error) {
	// Create FSM
	fsm := NewFSM(pool)

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

	node := &Node{
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
func (n *Node) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

// Leader returns the current leader address
func (n *Node) Leader() string {
	addr, _ := n.raft.LeaderWithID()
	return string(addr)
}

// AllocateBlock allocates a new IP block for a node
// This goes through Raft consensus
func (n *Node) AllocateBlock(nodeID string) (map[string]interface{}, error) {
	cmd := Command{
		Type:   CommandAllocateBlock,
		NodeID: nodeID,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(data, 10*time.Second)
	if err := future.Error(); err != nil {
		return nil, fmt.Errorf("raft apply failed: %w", err)
	}

	response := future.Response().(*FSMResponse)
	if !response.Success {
		return nil, fmt.Errorf("command failed: %s", response.Error)
	}

	return response.Data, nil
}

// ReleaseBlock releases an IP block from a node
func (n *Node) ReleaseBlock(nodeID, cidr string) error {
	releaseData := ReleaseBlockData{CIDR: cidr}
	dataBytes, err := json.Marshal(releaseData)
	if err != nil {
		return fmt.Errorf("failed to marshal release data: %w", err)
	}

	cmd := Command{
		Type:   CommandReleaseBlock,
		NodeID: nodeID,
		Data:   dataBytes,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(data, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("raft apply failed: %w", err)
	}

	response := future.Response().(*FSMResponse)
	if !response.Success {
		return fmt.Errorf("command failed: %s", response.Error)
	}

	return nil
}

// Join adds a new node to the Raft cluster
func (n *Node) Join(nodeID, addr string) error {
	if !n.IsLeader() {
		return fmt.Errorf("not the leader")
	}

	future := n.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 0)
	return future.Error()
}

// Leave removes a node from the Raft cluster
func (n *Node) Leave(nodeID string) error {
	if !n.IsLeader() {
		return fmt.Errorf("not the leader")
	}

	future := n.raft.RemoveServer(raft.ServerID(nodeID), 0, 0)
	return future.Error()
}

// Stats returns Raft statistics
func (n *Node) Stats() map[string]string {
	return n.raft.Stats()
}

// Shutdown gracefully shuts down the Raft node
func (n *Node) Shutdown() error {
	future := n.raft.Shutdown()
	return future.Error()
}

// GetPool returns the underlying IP pool
// This allows read-only access to pool state
func (n *Node) GetPool() *ipam.Pool {
	return n.pool
}
