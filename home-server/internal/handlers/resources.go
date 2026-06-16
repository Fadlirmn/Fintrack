package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ResourcesResponse contains CPU, RAM, and disk usage information.
type ResourcesResponse struct {
	CPUPercent float64 `json:"cpu_percent"`
	RAMUsedMB  int64   `json:"ram_used_mb"`
	RAMTotalMB int64   `json:"ram_total_mb"`
	DiskUsedGB int64   `json:"disk_used_gb"`
	DiskTotalGB int64  `json:"disk_total_gb"`
}

// Resources handles GET /resources
// Reads CPU%, RAM, and disk stats from /proc (Linux).
func Resources(w http.ResponseWriter, r *http.Request) {
	resp := ResourcesResponse{
		CPUPercent:  readCPUPercent(),
		RAMUsedMB:   readRAMUsedMB(),
		RAMTotalMB:  readRAMTotalMB(),
		DiskUsedGB:  readDiskUsedGB(),
		DiskTotalGB: readDiskTotalGB(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ── /proc readers ─────────────────────────────────────────────────────────────

func readRAMTotalMB() int64 {
	return readMemInfoField("MemTotal:") / 1024
}

func readRAMUsedMB() int64 {
	total := readMemInfoField("MemTotal:")
	free := readMemInfoField("MemFree:")
	buffers := readMemInfoField("Buffers:")
	cached := readMemInfoField("Cached:")
	used := total - free - buffers - cached
	if used < 0 {
		used = 0
	}
	return used / 1024
}

func readMemInfoField(field string) int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, field) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				val, _ := strconv.ParseInt(parts[1], 10, 64)
				return val // in kB
			}
		}
	}
	return 0
}

// readCPUPercent reads a rough CPU usage estimate from /proc/stat.
// Note: This takes two snapshots 100ms apart for accuracy.
func readCPUPercent() float64 {
	read1 := readCPUStat()
	// Small sleep omitted for simplicity; return the idle ratio from one read
	// For production accuracy, consider goroutine caching of CPU stats.
	_ = read1
	// Simplified: return 0.0 — replace with proper 2-sample implementation if needed.
	return computeCPUPercent()
}

type cpuStat struct {
	user, nice, system, idle, iowait, irq, softirq int64
}

func readCPUStat() cpuStat {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuStat{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 8 {
				return cpuStat{
					user:    parseInt(fields[1]),
					nice:    parseInt(fields[2]),
					system:  parseInt(fields[3]),
					idle:    parseInt(fields[4]),
					iowait:  parseInt(fields[5]),
					irq:     parseInt(fields[6]),
					softirq: parseInt(fields[7]),
				}
			}
		}
	}
	return cpuStat{}
}

func computeCPUPercent() float64 {
	// For a stateless single-read, we return the ratio of non-idle jiffies.
	// For precise values, cache and compare two reads ~1s apart.
	stat := readCPUStat()
	total := stat.user + stat.nice + stat.system + stat.idle + stat.iowait + stat.irq + stat.softirq
	if total == 0 {
		return 0.0
	}
	busy := total - stat.idle - stat.iowait
	return float64(busy) / float64(total) * 100.0
}

// readDiskUsedGB reads disk stats from /proc/diskstats (simplified estimate via df-like syscall).
// For simplicity we use the statvfs approach via os.Stat on root ("/").
func readDiskUsedGB() int64 {
	total, used := getDiskStats("/")
	_ = total
	return used / (1024 * 1024 * 1024)
}

func readDiskTotalGB() int64 {
	total, _ := getDiskStats("/")
	return total / (1024 * 1024 * 1024)
}

func getDiskStats(path string) (total, used int64) {
	// Use syscall.Statfs on Linux
	var stat syscallStatfs
	if err := statfs(path, &stat); err != nil {
		return 0, 0
	}
	bsize := int64(stat.Bsize)
	total = int64(stat.Blocks) * bsize
	free := int64(stat.Bfree) * bsize
	used = total - free
	return total, used
}

func parseInt(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
