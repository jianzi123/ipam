package store

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/boltdb/bolt"
)

func TestStore(t *testing.T) {
	// Create temporary database
	dbPath := "/tmp/ipam_test.db"
	defer os.Remove(dbPath)

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	t.Run("Save and get IP mapping", func(t *testing.T) {
		mapping := IPMapping{
			ContainerID:  "container-123",
			PodName:      "test-pod",
			PodNamespace: "default",
			NodeID:       "node1",
			IP:           "10.244.1.5",
			CIDR:         "10.244.1.5/24",
			BlockCIDR:    "10.244.1.0/24",
		}

		// Save mapping
		if err := store.SaveIPMapping(mapping); err != nil {
			t.Fatalf("Failed to save mapping: %v", err)
		}

		// Get mapping
		retrieved, err := store.GetIPMapping("container-123")
		if err != nil {
			t.Fatalf("Failed to get mapping: %v", err)
		}

		if retrieved.ContainerID != mapping.ContainerID {
			t.Errorf("Expected container ID %s, got %s", mapping.ContainerID, retrieved.ContainerID)
		}
		if retrieved.IP != mapping.IP {
			t.Errorf("Expected IP %s, got %s", mapping.IP, retrieved.IP)
		}
	})

	t.Run("List mappings by node", func(t *testing.T) {
		// Add another mapping
		mapping2 := IPMapping{
			ContainerID:  "container-456",
			PodName:      "test-pod-2",
			PodNamespace: "default",
			NodeID:       "node1",
			IP:           "10.244.1.6",
			CIDR:         "10.244.1.6/24",
			BlockCIDR:    "10.244.1.0/24",
		}
		store.SaveIPMapping(mapping2)

		// List by node
		mappings, err := store.ListMappingsByNode("node1")
		if err != nil {
			t.Fatalf("Failed to list mappings: %v", err)
		}

		if len(mappings) != 2 {
			t.Errorf("Expected 2 mappings for node1, got %d", len(mappings))
		}
	})

	t.Run("Get mapping by IP", func(t *testing.T) {
		mapping, err := store.GetMappingByIP("10.244.1.5")
		if err != nil {
			t.Fatalf("Failed to get mapping by IP: %v", err)
		}

		if mapping.ContainerID != "container-123" {
			t.Errorf("Expected container-123, got %s", mapping.ContainerID)
		}
	})

	t.Run("Delete mapping", func(t *testing.T) {
		if err := store.DeleteIPMapping("container-123"); err != nil {
			t.Fatalf("Failed to delete mapping: %v", err)
		}

		_, err := store.GetIPMapping("container-123")
		if err == nil {
			t.Error("Expected error when getting deleted mapping")
		}
	})

	t.Run("Get stats", func(t *testing.T) {
		stats, err := store.GetStats()
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}

		if stats.TotalMappings != 1 { // Only container-456 remains
			t.Errorf("Expected 1 total mapping, got %d", stats.TotalMappings)
		}

		if stats.MappingsByNode["node1"] != 1 {
			t.Errorf("Expected 1 mapping for node1, got %d", stats.MappingsByNode["node1"])
		}
	})

	t.Run("Cleanup stale entries", func(t *testing.T) {
		// Add an old mapping
		oldMapping := IPMapping{
			ContainerID:  "old-container",
			PodName:      "old-pod",
			PodNamespace: "default",
			NodeID:       "node1",
			IP:           "10.244.1.7",
			CIDR:         "10.244.1.7/24",
			BlockCIDR:    "10.244.1.0/24",
			AllocatedAt:  time.Now().Add(-2 * time.Hour),
		}

		// Manually save with old timestamp
		store.db.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte(bucketIPMappings))
			data, _ := json.Marshal(oldMapping)
			return bucket.Put([]byte(oldMapping.ContainerID), data)
		})

		// Cleanup entries older than 1 hour
		deleted, err := store.CleanupStaleEntries(1 * time.Hour)
		if err != nil {
			t.Fatalf("Failed to cleanup: %v", err)
		}

		if deleted != 1 {
			t.Errorf("Expected 1 deleted entry, got %d", deleted)
		}
	})
}
