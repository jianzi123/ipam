package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jianzi123/ipam/pkg/ipam"
	"github.com/jianzi123/ipam/pkg/metrics"
	"github.com/jianzi123/ipam/pkg/raft"
	"github.com/jianzi123/ipam/pkg/server"
	"github.com/jianzi123/ipam/pkg/store"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	nodeID       = flag.String("node-id", "", "Unique node identifier")
	bindAddr     = flag.String("bind-addr", "0.0.0.0:7000", "Raft bind address")
	dataDir      = flag.String("data-dir", "/var/lib/ipam", "Data directory")
	bootstrap    = flag.Bool("bootstrap", false, "Bootstrap new cluster")
	joinAddr     = flag.String("join", "", "Address of node to join")
	clusterCIDR  = flag.String("cluster-cidr", "10.244.0.0/16", "Cluster CIDR")
	blockSize    = flag.Int("block-size", 24, "IP block size (CIDR prefix)")
	grpcAddr     = flag.String("grpc-addr", "0.0.0.0:9090", "gRPC server address")
	unixSocket   = flag.String("unix-socket", "/run/ipam/ipam.sock", "Unix socket path")
	metricsAddr  = flag.String("metrics-addr", "0.0.0.0:2112", "Prometheus metrics address")
	enableStore  = flag.Bool("enable-store", true, "Enable persistent IP mapping store")
)

func main() {
	flag.Parse()

	// Validate flags
	if *nodeID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Fatal("node-id is required or hostname must be available")
		}
		*nodeID = hostname
	}

	log.Printf("Starting IPAM daemon...")
	log.Printf("  Node ID: %s", *nodeID)
	log.Printf("  Bind Address: %s", *bindAddr)
	log.Printf("  Data Directory: %s", *dataDir)
	log.Printf("  Cluster CIDR: %s", *clusterCIDR)
	log.Printf("  Block Size: /%d", *blockSize)

	// Create IP pool
	pool, err := ipam.NewPool(ipam.PoolConfig{
		ClusterCIDR: *clusterCIDR,
		BlockSize:   *blockSize,
	})
	if err != nil {
		log.Fatalf("Failed to create IP pool: %v", err)
	}

	// Create Raft node
	raftNode, err := raft.NewNode(&raft.NodeConfig{
		NodeID:    *nodeID,
		BindAddr:  *bindAddr,
		DataDir:   *dataDir,
		Bootstrap: *bootstrap,
		JoinAddr:  *joinAddr,
		HeartbeatTimeout: 1 * time.Second,
		ElectionTimeout:  1 * time.Second,
		CommitTimeout:    1 * time.Second,
	}, pool)
	if err != nil {
		log.Fatalf("Failed to create Raft node: %v", err)
	}

	log.Printf("Raft node created successfully")

	// Wait for leader election if not bootstrapping
	if !*bootstrap {
		log.Printf("Waiting for leader election...")
		for i := 0; i < 10; i++ {
			if raftNode.Leader() != "" {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}

	if raftNode.IsLeader() {
		log.Printf("This node is the LEADER")
	} else {
		log.Printf("Leader is: %s", raftNode.Leader())
	}

	// Initialize persistent store if enabled
	var ipamStore *store.Store
	if *enableStore {
		storePath := fmt.Sprintf("%s/ipam.db", *dataDir)
		ipamStore, err = store.NewStore(storePath)
		if err != nil {
			log.Printf("Warning: failed to create store: %v", err)
		} else {
			log.Printf("Persistent store initialized at %s", storePath)
			defer ipamStore.Close()
		}
	}

	// Initialize metrics
	metricsCollector := metrics.NewMetrics()
	log.Printf("Metrics initialized")

	// Start metrics collector
	collector := metrics.NewCollector(metricsCollector, pool, raftNode, ipamStore, 10*time.Second)
	collector.Start()
	defer collector.Stop()
	log.Printf("Metrics collector started")

	// Start Prometheus metrics server
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Printf("Starting metrics server on %s", *metricsAddr)
		if err := http.ListenAndServe(*metricsAddr, nil); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	// Create gRPC server
	grpcServer := server.NewServer(pool, raftNode, ipamStore)

	// Start gRPC server on Unix socket
	go func() {
		log.Printf("Starting gRPC server on Unix socket %s", *unixSocket)
		if err := grpcServer.StartUnix(*unixSocket); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	// Also start on TCP for remote access
	go func() {
		log.Printf("Starting gRPC server on TCP %s", *grpcAddr)
		if err := grpcServer.Start(*grpcAddr); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	// Print initial pool stats
	stats := pool.GetStats()
	log.Printf("Pool initialized: %s", stats.String())

	// Example: Allocate a block for this node (for testing)
	if raftNode.IsLeader() {
		log.Printf("Allocating test block for node: %s", *nodeID)
		blockInfo, err := raftNode.AllocateBlock(*nodeID)
		if err != nil {
			log.Printf("Failed to allocate block: %v", err)
		} else {
			log.Printf("Allocated block: %v", blockInfo)
			stats = pool.GetStats()
			log.Printf("Pool stats after allocation: %s", stats.String())
		}
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("IPAM daemon running. Metrics: http://%s/metrics", *metricsAddr)
	log.Printf("Press Ctrl+C to stop.")
	<-sigCh

	log.Printf("Shutting down...")

	// Stop gRPC server
	grpcServer.Stop()
	log.Printf("gRPC server stopped")

	// Stop Raft node
	if err := raftNode.Shutdown(); err != nil {
		log.Printf("Error during Raft shutdown: %v", err)
	}

	log.Printf("Shutdown complete")
}
