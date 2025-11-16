package main

import (
	"flag"
	"fmt"
	"os"
)

var (
	daemonAddr = flag.String("daemon", "localhost:9090", "IPAM daemon address")
)

func main() {
	flag.Parse()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "stats":
		handleStats()
	case "blocks":
		handleBlocks()
	case "allocate":
		handleAllocate()
	case "release":
		handleRelease()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("IPAM CLI - IP Address Management Command Line Tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  ipam-cli [options] <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  stats              Show pool statistics")
	fmt.Println("  blocks <node-id>   Show blocks for a node")
	fmt.Println("  allocate <node-id> Allocate a block for a node")
	fmt.Println("  release <node-id> <cidr>  Release a block")
	fmt.Println()
	fmt.Println("Options:")
	flag.PrintDefaults()
}

func handleStats() {
	fmt.Println("Pool Statistics:")
	fmt.Println("  (Not implemented - would connect to gRPC daemon)")
	// TODO: Implement gRPC call to GetPoolStats
}

func handleBlocks() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: ipam-cli blocks <node-id>")
		os.Exit(1)
	}

	nodeID := os.Args[2]
	fmt.Printf("Blocks for node %s:\n", nodeID)
	fmt.Println("  (Not implemented - would connect to gRPC daemon)")
	// TODO: Implement gRPC call to GetNodeBlocks
}

func handleAllocate() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: ipam-cli allocate <node-id>")
		os.Exit(1)
	}

	nodeID := os.Args[2]
	fmt.Printf("Allocating block for node %s...\n", nodeID)
	fmt.Println("  (Not implemented - would connect to gRPC daemon)")
	// TODO: Implement gRPC call to AllocateBlock
}

func handleRelease() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: ipam-cli release <node-id> <cidr>")
		os.Exit(1)
	}

	nodeID := os.Args[2]
	cidr := os.Args[3]
	fmt.Printf("Releasing block %s from node %s...\n", cidr, nodeID)
	fmt.Println("  (Not implemented - would connect to gRPC daemon)")
	// TODO: Implement gRPC call to ReleaseBlock
}
