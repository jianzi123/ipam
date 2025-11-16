package server

import (
	"context"
	"fmt"
	"net"

	"github.com/jianzi123/ipam/pkg/allocator"
	"github.com/jianzi123/ipam/pkg/ipam"
	"github.com/jianzi123/ipam/pkg/raft"
	"github.com/jianzi123/ipam/pkg/store"
	"google.golang.org/grpc"
)

// IPAMServer implements the IPAM gRPC service
type IPAMServer struct {
	pool     *ipam.Pool
	raftNode *raft.Node
	store    *store.Store
}

// NewIPAMServer creates a new IPAM server
func NewIPAMServer(pool *ipam.Pool, raftNode *raft.Node, store *store.Store) *IPAMServer {
	return &IPAMServer{
		pool:     pool,
		raftNode: raftNode,
		store:    store,
	}
}

// AllocateIPRequest represents IP allocation request
type AllocateIPRequest struct {
	NodeID       string
	PodName      string
	PodNamespace string
	ContainerID  string
}

// AllocateIPResponse represents IP allocation response
type AllocateIPResponse struct {
	IP      string
	CIDR    string
	Gateway string
	Routes  []Route
}

// Route represents a routing entry
type Route struct {
	Dst string
	GW  string
}

// ReleaseIPRequest represents IP release request
type ReleaseIPRequest struct {
	NodeID      string
	IP          string
	ContainerID string
}

// ReleaseIPResponse represents IP release response
type ReleaseIPResponse struct {
	Success bool
	Message string
}

// AllocateIP allocates an IP address for a pod
func (s *IPAMServer) AllocateIP(ctx context.Context, req *AllocateIPRequest) (*AllocateIPResponse, error) {
	// Allocate IP from pool
	ip, block, err := s.pool.AllocateIPForNode(req.NodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate IP: %w", err)
	}

	// Calculate CIDR notation
	ones, _ := block.CIDR.Mask.Size()
	cidr := fmt.Sprintf("%s/%d", ip.String(), ones)

	// Save container ID -> IP mapping
	if s.store != nil {
		mapping := store.IPMapping{
			ContainerID:  req.ContainerID,
			PodName:      req.PodName,
			PodNamespace: req.PodNamespace,
			NodeID:       req.NodeID,
			IP:           ip.String(),
			CIDR:         cidr,
			BlockCIDR:    block.CIDR.String(),
		}
		if err := s.store.SaveIPMapping(mapping); err != nil {
			// Log error but don't fail the allocation
			fmt.Printf("Warning: failed to save IP mapping: %v\n", err)
		}
	}

	// Calculate gateway IP (first usable IP in block)
	gatewayIP := s.calculateGateway(block.CIDR)

	response := &AllocateIPResponse{
		IP:      ip.String(),
		CIDR:    fmt.Sprintf("%s/%d", ip.String(), ones),
		Gateway: gatewayIP,
		Routes: []Route{
			{Dst: "0.0.0.0/0", GW: ""},
		},
	}

	// Async: Check if we need to allocate a new block (< 20% remaining)
	go s.checkAndAllocateBlock(req.NodeID, block)

	return response, nil
}

// ReleaseIP releases an IP address
func (s *IPAMServer) ReleaseIP(ctx context.Context, req *ReleaseIPRequest) (*ReleaseIPResponse, error) {
	ip := net.ParseIP(req.IP)
	if ip == nil {
		return &ReleaseIPResponse{
			Success: false,
			Message: fmt.Sprintf("invalid IP address: %s", req.IP),
		}, nil
	}

	// Release IP from pool
	if err := s.pool.ReleaseIP(ip, req.NodeID); err != nil {
		return &ReleaseIPResponse{
			Success: false,
			Message: fmt.Sprintf("failed to release IP: %v", err),
		}, nil
	}

	// Remove container ID -> IP mapping
	if s.store != nil {
		if err := s.store.DeleteIPMapping(req.ContainerID); err != nil {
			// Log error but don't fail the release
			fmt.Printf("Warning: failed to delete IP mapping: %v\n", err)
		}
	}

	return &ReleaseIPResponse{
		Success: true,
		Message: "IP released successfully",
	}, nil
}

// GetNodeBlocks returns all IP blocks for a node
func (s *IPAMServer) GetNodeBlocks(ctx context.Context, nodeID string) ([]*BlockInfo, error) {
	blocks, err := s.pool.GetNodeBlocks(nodeID)
	if err != nil {
		return nil, err
	}

	result := make([]*BlockInfo, len(blocks))
	for i, block := range blocks {
		result[i] = &BlockInfo{
			CIDR:      block.CIDR.String(),
			NodeID:    block.NodeID,
			Total:     block.Total,
			Used:      block.Used,
			Available: block.Available(),
			CreatedAt: block.CreatedAt.Unix(),
		}
	}

	return result, nil
}

// GetPoolStats returns pool statistics
func (s *IPAMServer) GetPoolStats(ctx context.Context) (*PoolStatsResponse, error) {
	stats := s.pool.GetStats()

	nodeStats := make(map[string]*NodeStatsInfo)
	for nodeID, ns := range stats.NodeStats {
		nodeStats[nodeID] = &NodeStatsInfo{
			NodeID:       ns.NodeID,
			Blocks:       ns.Blocks,
			TotalIPs:     ns.TotalIPs,
			UsedIPs:      ns.UsedIPs,
			AvailableIPs: ns.AvailableIPs,
		}
	}

	return &PoolStatsResponse{
		TotalNodes:   stats.TotalNodes,
		TotalBlocks:  stats.TotalBlocks,
		TotalIPs:     stats.TotalIPs,
		UsedIPs:      stats.UsedIPs,
		AvailableIPs: stats.AvailableIPs,
		NodeStats:    nodeStats,
	}, nil
}

// BlockInfo represents IP block information
type BlockInfo struct {
	CIDR      string
	NodeID    string
	Total     int
	Used      int
	Available int
	CreatedAt int64
}

// PoolStatsResponse represents pool statistics
type PoolStatsResponse struct {
	TotalNodes   int
	TotalBlocks  int
	TotalIPs     int
	UsedIPs      int
	AvailableIPs int
	NodeStats    map[string]*NodeStatsInfo
}

// NodeStatsInfo represents node statistics
type NodeStatsInfo struct {
	NodeID       string
	Blocks       int
	TotalIPs     int
	UsedIPs      int
	AvailableIPs int
}

// checkAndAllocateBlock checks if a new block is needed and allocates it
func (s *IPAMServer) checkAndAllocateBlock(nodeID string, currentBlock *allocator.IPBlock) {
	// Check if remaining capacity < 20%
	if currentBlock.Available() < int(float64(currentBlock.Total)*0.2) {
		// Allocate new block through Raft
		if s.raftNode != nil && s.raftNode.IsLeader() {
			_, err := s.raftNode.AllocateBlock(nodeID)
			if err != nil {
				fmt.Printf("Warning: failed to pre-allocate block for node %s: %v\n", nodeID, err)
			}
		}
	}
}

// calculateGateway calculates the gateway IP for a block
func (s *IPAMServer) calculateGateway(cidr *net.IPNet) string {
	// Gateway is typically the first usable IP in the subnet
	ip := cidr.IP.To4()
	if ip == nil {
		return ""
	}

	// First usable IP (network address + 1)
	addr := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	addr += 1

	gatewayIP := net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr))
	return gatewayIP.String()
}

// Server represents the gRPC server
type Server struct {
	grpcServer *grpc.Server
	ipamServer *IPAMServer
	listener   net.Listener
}

// NewServer creates a new gRPC server
func NewServer(pool *ipam.Pool, raftNode *raft.Node, store *store.Store) *Server {
	ipamServer := NewIPAMServer(pool, raftNode, store)
	grpcServer := grpc.NewServer()

	return &Server{
		grpcServer: grpcServer,
		ipamServer: ipamServer,
	}
}

// Start starts the gRPC server on the given address
func (s *Server) Start(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	s.listener = listener
	return s.grpcServer.Serve(listener)
}

// StartUnix starts the gRPC server on a Unix socket
func (s *Server) StartUnix(socketPath string) error {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}

	s.listener = listener
	return s.grpcServer.Serve(listener)
}

// Stop stops the gRPC server
func (s *Server) Stop() {
	s.grpcServer.GracefulStop()
}

// GetIPAMServer returns the IPAM server instance
func (s *Server) GetIPAMServer() *IPAMServer {
	return s.ipamServer
}
