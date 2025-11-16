package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/raft"
	"github.com/jianzi123/ipam/pkg/ipam"
)

// FSM implements the Raft Finite State Machine for IPAM
// It manages the replicated state of IP block allocations
type FSM struct {
	pool *ipam.Pool
	mu   sync.RWMutex
}

// CommandType represents the type of Raft command
type CommandType string

const (
	CommandAllocateBlock CommandType = "allocate_block"
	CommandReleaseBlock  CommandType = "release_block"
	CommandUpdateUsage   CommandType = "update_usage"
)

// Command represents a Raft log command
type Command struct {
	Type   CommandType     `json:"type"`
	NodeID string          `json:"node_id"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// AllocateBlockData contains data for block allocation
type AllocateBlockData struct {
	CIDR string `json:"cidr"`
}

// ReleaseBlockData contains data for block release
type ReleaseBlockData struct {
	CIDR string `json:"cidr"`
}

// UpdateUsageData contains usage update information
type UpdateUsageData struct {
	CIDR      string `json:"cidr"`
	UsedCount int    `json:"used_count"`
}

// NewFSM creates a new IPAM FSM
func NewFSM(pool *ipam.Pool) *FSM {
	return &FSM{
		pool: pool,
	}
}

// Apply applies a Raft log entry to the FSM
// This is called by Raft when a command is committed
func (f *FSM) Apply(log *raft.Log) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	var cmd Command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal command: %v", err)}
	}

	switch cmd.Type {
	case CommandAllocateBlock:
		return f.applyAllocateBlock(cmd)
	case CommandReleaseBlock:
		return f.applyReleaseBlock(cmd)
	case CommandUpdateUsage:
		return f.applyUpdateUsage(cmd)
	default:
		return &FSMResponse{Success: false, Error: fmt.Sprintf("unknown command type: %s", cmd.Type)}
	}
}

// applyAllocateBlock allocates a new IP block for a node
func (f *FSM) applyAllocateBlock(cmd Command) interface{} {
	block, err := f.pool.AllocateBlockForNode(cmd.NodeID)
	if err != nil {
		return &FSMResponse{Success: false, Error: err.Error()}
	}

	return &FSMResponse{
		Success: true,
		Data: map[string]interface{}{
			"cidr":     block.CIDR.String(),
			"node_id":  block.NodeID,
			"total":    block.Total,
			"used":     block.Used,
			"available": block.Available(),
		},
	}
}

// applyReleaseBlock releases an IP block from a node
func (f *FSM) applyReleaseBlock(cmd Command) interface{} {
	var data ReleaseBlockData
	if err := json.Unmarshal(cmd.Data, &data); err != nil {
		return &FSMResponse{Success: false, Error: fmt.Sprintf("failed to unmarshal data: %v", err)}
	}

	if err := f.pool.ReleaseBlockForNode(cmd.NodeID, data.CIDR); err != nil {
		return &FSMResponse{Success: false, Error: err.Error()}
	}

	return &FSMResponse{Success: true}
}

// applyUpdateUsage updates usage statistics for a block
func (f *FSM) applyUpdateUsage(cmd Command) interface{} {
	// For now, usage is tracked locally in blocks
	// This could be extended to replicate usage stats if needed
	return &FSMResponse{Success: true}
}

// Snapshot returns a snapshot of the FSM state
// This is used for log compaction
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Get current pool stats to snapshot
	stats := f.pool.GetStats()

	return &FSMSnapshot{
		stats: stats,
	}, nil
}

// Restore restores the FSM state from a snapshot
func (f *FSM) Restore(snapshot io.ReadCloser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	defer snapshot.Close()

	// Read snapshot data
	var snapshotData SnapshotData
	decoder := json.NewDecoder(snapshot)
	if err := decoder.Decode(&snapshotData); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Restore pool state by re-creating blocks
	// In a real implementation, you'd restore the full pool state
	// For now, we'll keep the existing pool and log the restore
	// This is a simplified version

	return nil
}

// FSMResponse represents the response from an FSM apply operation
type FSMResponse struct {
	Success bool                   `json:"success"`
	Error   string                 `json:"error,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// FSMSnapshot represents a point-in-time snapshot of the FSM
type FSMSnapshot struct {
	stats ipam.PoolStats
}

// Persist writes the snapshot to the given sink
func (s *FSMSnapshot) Persist(sink raft.SnapshotSink) error {
	// Convert stats to snapshot data
	data := SnapshotData{
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
func (s *FSMSnapshot) Release() {
	// Nothing to release in this simple implementation
}

// SnapshotData represents the data stored in a snapshot
type SnapshotData struct {
	Stats ipam.PoolStats `json:"stats"`
}
