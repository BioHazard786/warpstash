package storage

// DiskSpaceInfo contains physical filesystem capacity metrics.
type DiskSpaceInfo struct {
	TotalBytes     uint64 `json:"total_bytes"`
	FreeBytes      uint64 `json:"free_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

const (
	// DefaultSafetyReserveBytes reserves 50MB of disk space for the OS,
	// SQLite WAL journaling, and system maintenance.
	DefaultSafetyReserveBytes = 50 * 1024 * 1024
)
