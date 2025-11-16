package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/jianzi123/ipam/pkg/cni"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Environment variables passed by container runtime (CNI spec)
const (
	EnvCommand     = "CNI_COMMAND"
	EnvContainerID = "CNI_CONTAINERID"
	EnvNetNS       = "CNI_NETNS"
	EnvIFName      = "CNI_IFNAME"
	EnvArgs        = "CNI_ARGS"
	EnvPath        = "CNI_PATH"
)

func main() {
	// Read CNI command from environment
	command := os.Getenv(EnvCommand)
	if command == "" {
		printError(cni.ErrCodeInvalidEnvironmentVar, "CNI_COMMAND not set", "")
		os.Exit(1)
	}

	// Execute command
	switch command {
	case "ADD":
		handleAdd()
	case "DEL":
		handleDel()
	case "CHECK":
		handleCheck()
	case "VERSION":
		handleVersion()
	default:
		printError(cni.ErrCodeInvalidEnvironmentVar, "unknown command", command)
		os.Exit(1)
	}
}

// handleAdd allocates an IP and configures the interface
func handleAdd() {
	// Read network configuration from stdin
	netConf, err := loadNetConf()
	if err != nil {
		printError(cni.ErrCodeDecodingFailure, "failed to load network config", err.Error())
		os.Exit(1)
	}

	// Get environment variables
	containerID := os.Getenv(EnvContainerID)
	netns := os.Getenv(EnvNetNS)
	ifname := os.Getenv(EnvIFName)

	if containerID == "" || netns == "" || ifname == "" {
		printError(cni.ErrCodeInvalidEnvironmentVar, "missing required env vars", "")
		os.Exit(1)
	}

	// Get node ID (from hostname by default)
	nodeID, err := os.Hostname()
	if err != nil {
		nodeID = "unknown"
	}

	// Allocate IP from IPAM daemon
	ipResult, err := allocateIP(netConf, nodeID, containerID)
	if err != nil {
		printError(cni.ErrCodeInternal, "failed to allocate IP", err.Error())
		os.Exit(1)
	}

	// Return result
	result := &cni.Result{
		CNIVersion: netConf.CNIVersion,
		IPs: []*cni.IPConfig{
			{
				Address: ipResult.CIDR,
				Gateway: ipResult.Gateway,
			},
		},
		Routes: convertRoutes(ipResult.Routes),
	}

	if err := result.Print(); err != nil {
		printError(cni.ErrCodeInternal, "failed to print result", err.Error())
		os.Exit(1)
	}
}

// handleDel releases an IP
func handleDel() {
	// Read network configuration
	netConf, err := loadNetConf()
	if err != nil {
		// For DEL, we should not fail if config is invalid
		// Just exit successfully
		os.Exit(0)
	}

	containerID := os.Getenv(EnvContainerID)
	if containerID == "" {
		os.Exit(0)
	}

	// Get node ID
	nodeID, err := os.Hostname()
	if err != nil {
		nodeID = "unknown"
	}

	// Release IP
	// Note: In a real implementation, we'd need to track container ID -> IP mapping
	// For now, this is simplified
	_ = releaseIP(netConf, nodeID, containerID)

	// DEL always succeeds
	os.Exit(0)
}

// handleCheck validates the interface configuration
func handleCheck() {
	// Read network configuration
	netConf, err := loadNetConf()
	if err != nil {
		printError(cni.ErrCodeDecodingFailure, "failed to load network config", err.Error())
		os.Exit(1)
	}

	// For now, just validate that config is parseable
	_ = netConf

	// CHECK succeeds if no errors
	os.Exit(0)
}

// handleVersion returns supported CNI versions
func handleVersion() {
	result := cni.NewVersionResult()
	if err := result.Print(); err != nil {
		printError(cni.ErrCodeInternal, "failed to print version", err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

// loadNetConf loads network configuration from stdin
func loadNetConf() (*cni.NetConf, error) {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdin: %w", err)
	}

	var conf cni.NetConf
	if err := json.Unmarshal(data, &conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &conf, nil
}

// allocateIP allocates an IP from the IPAM daemon
func allocateIP(netConf *cni.NetConf, nodeID, containerID string) (*IPAMResult, error) {
	// Connect to IPAM daemon
	socket := netConf.IPAM.DaemonSocket
	if socket == "" {
		socket = "/run/ipam/ipam.sock"
	}

	// Create gRPC connection (simplified - no proto for now)
	// In real implementation, this would use the generated gRPC client
	conn, err := grpc.Dial(
		socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IPAM daemon: %w", err)
	}
	defer conn.Close()

	// For now, return a mock result
	// In real implementation, this would call the gRPC AllocateIP method
	return &IPAMResult{
		IP:      "10.244.1.5",
		CIDR:    "10.244.1.5/24",
		Gateway: "10.244.1.1",
		Routes:  []RouteInfo{{Dst: "0.0.0.0/0"}},
	}, nil
}

// releaseIP releases an IP to the IPAM daemon
func releaseIP(netConf *cni.NetConf, nodeID, containerID string) error {
	// Similar to allocateIP, would use gRPC in real implementation
	return nil
}

// IPAMResult represents the result from IPAM
type IPAMResult struct {
	IP      string
	CIDR    string
	Gateway string
	Routes  []RouteInfo
}

// RouteInfo represents route information
type RouteInfo struct {
	Dst string
	GW  string
}

// convertRoutes converts RouteInfo to CNI Route
func convertRoutes(routes []RouteInfo) []*cni.Route {
	result := make([]*cni.Route, len(routes))
	for i, r := range routes {
		result[i] = &cni.Route{
			Dst: r.Dst,
			GW:  r.GW,
		}
	}
	return result
}

// printError prints a CNI error and exits
func printError(code uint, msg, details string) {
	err := cni.NewError(code, msg, details)
	err.Print()
}
