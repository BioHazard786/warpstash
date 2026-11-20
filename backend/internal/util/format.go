package util

import "fmt"

// FormatBytes formats a byte count into a human-readable string (e.g., "512 B", "10.0 MB", "1.0 GB").
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		if b < 0 {
			return "0 B"
		}
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
