package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/boltdb/bolt"
)

const (
	// Bucket names
	bucketIPMappings = "ip_mappings"
	bucketMetadata   = "metadata"
)

// Store manages persistent storage for IPAM
type Store struct {
	db *bolt.DB
}

// IPMapping represents a container ID to IP mapping
type IPMapping struct {
	ContainerID  string    `json:"container_id"`
	PodName      string    `json:"pod_name"`
	PodNamespace string    `json:"pod_namespace"`
	NodeID       string    `json:"node_id"`
	IP           string    `json:"ip"`
	CIDR         string    `json:"cidr"`
	BlockCIDR    string    `json:"block_cidr"`
	AllocatedAt  time.Time `json:"allocated_at"`
}

// NewStore creates a new store
func NewStore(dbPath string) (*Store, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create buckets
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketIPMappings)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketMetadata)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the store
func (s *Store) Close() error {
	return s.db.Close()
}

// SaveIPMapping saves a container ID to IP mapping
func (s *Store) SaveIPMapping(mapping IPMapping) error {
	mapping.AllocatedAt = time.Now()

	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		data, err := json.Marshal(mapping)
		if err != nil {
			return fmt.Errorf("failed to marshal mapping: %w", err)
		}

		return bucket.Put([]byte(mapping.ContainerID), data)
	})
}

// GetIPMapping retrieves a mapping by container ID
func (s *Store) GetIPMapping(containerID string) (*IPMapping, error) {
	var mapping IPMapping

	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		data := bucket.Get([]byte(containerID))
		if data == nil {
			return fmt.Errorf("mapping not found for container %s", containerID)
		}

		return json.Unmarshal(data, &mapping)
	})

	if err != nil {
		return nil, err
	}

	return &mapping, nil
}

// DeleteIPMapping deletes a mapping by container ID
func (s *Store) DeleteIPMapping(containerID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		return bucket.Delete([]byte(containerID))
	})
}

// ListIPMappings returns all IP mappings
func (s *Store) ListIPMappings() ([]IPMapping, error) {
	var mappings []IPMapping

	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		return bucket.ForEach(func(k, v []byte) error {
			var mapping IPMapping
			if err := json.Unmarshal(v, &mapping); err != nil {
				return err
			}
			mappings = append(mappings, mapping)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return mappings, nil
}

// ListMappingsByNode returns all IP mappings for a specific node
func (s *Store) ListMappingsByNode(nodeID string) ([]IPMapping, error) {
	var mappings []IPMapping

	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		return bucket.ForEach(func(k, v []byte) error {
			var mapping IPMapping
			if err := json.Unmarshal(v, &mapping); err != nil {
				return err
			}
			if mapping.NodeID == nodeID {
				mappings = append(mappings, mapping)
			}
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return mappings, nil
}

// GetMappingByIP finds a mapping by IP address
func (s *Store) GetMappingByIP(ip string) (*IPMapping, error) {
	var result *IPMapping

	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		return bucket.ForEach(func(k, v []byte) error {
			var mapping IPMapping
			if err := json.Unmarshal(v, &mapping); err != nil {
				return err
			}
			if mapping.IP == ip {
				result = &mapping
				return nil // Stop iteration
			}
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, fmt.Errorf("no mapping found for IP %s", ip)
	}

	return result, nil
}

// CleanupStaleEntries removes mappings older than the specified duration
func (s *Store) CleanupStaleEntries(maxAge time.Duration) (int, error) {
	deleted := 0
	cutoff := time.Now().Add(-maxAge)

	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		// Collect keys to delete
		var toDelete [][]byte
		bucket.ForEach(func(k, v []byte) error {
			var mapping IPMapping
			if err := json.Unmarshal(v, &mapping); err != nil {
				return nil // Skip malformed entries
			}
			if mapping.AllocatedAt.Before(cutoff) {
				toDelete = append(toDelete, k)
			}
			return nil
		})

		// Delete collected keys
		for _, key := range toDelete {
			if err := bucket.Delete(key); err != nil {
				return err
			}
			deleted++
		}

		return nil
	})

	return deleted, err
}

// GetStats returns store statistics
func (s *Store) GetStats() (*StoreStats, error) {
	stats := &StoreStats{}

	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIPMappings))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketIPMappings)
		}

		stats.TotalMappings = bucket.Stats().KeyN

		// Count by node
		nodeCount := make(map[string]int)
		bucket.ForEach(func(k, v []byte) error {
			var mapping IPMapping
			if err := json.Unmarshal(v, &mapping); err != nil {
				return nil
			}
			nodeCount[mapping.NodeID]++
			return nil
		})

		stats.MappingsByNode = nodeCount
		return nil
	})

	if err != nil {
		return nil, err
	}

	return stats, nil
}

// StoreStats represents store statistics
type StoreStats struct {
	TotalMappings  int
	MappingsByNode map[string]int
}
