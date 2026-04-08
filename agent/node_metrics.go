package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/jatinparab/homectl/common"
)

var cpuSample struct {
	mu       sync.Mutex
	lastIdle uint64
	lastAll  uint64
	hasPrev  bool
}

func collectNodeMetrics(appdataDir string) common.NodeMetrics {
	metrics := common.NodeMetrics{}

	metrics.CPUPercent = readCPUPercent()
	metrics.MemoryTotal, metrics.MemoryUsed, metrics.MemoryPercent = readMemoryUsage()
	metrics.DiskTotal, metrics.DiskUsed, metrics.DiskFree, metrics.DiskPercent = readDiskUsage(appdataDir)

	return metrics
}

func readCPUPercent() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}

	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}

	vals := make([]uint64, 0, len(fields)-1)
	for _, s := range fields[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0
		}
		vals = append(vals, v)
	}

	var total uint64
	for _, v := range vals {
		total += v
	}
	idle := vals[3]

	cpuSample.mu.Lock()
	defer cpuSample.mu.Unlock()

	if !cpuSample.hasPrev {
		cpuSample.lastAll = total
		cpuSample.lastIdle = idle
		cpuSample.hasPrev = true
		return 0
	}

	deltaAll := total - cpuSample.lastAll
	deltaIdle := idle - cpuSample.lastIdle
	cpuSample.lastAll = total
	cpuSample.lastIdle = idle

	if deltaAll == 0 {
		return 0
	}
	busy := float64(deltaAll-deltaIdle) / float64(deltaAll) * 100
	if busy < 0 {
		return 0
	}
	if busy > 100 {
		return 100
	}
	return busy
}

func readMemoryUsage() (total, used uint64, percent float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}
	defer f.Close()

	var memTotalKB uint64
	var memAvailKB uint64

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			memTotalKB = parseMeminfoKB(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			memAvailKB = parseMeminfoKB(line)
		}
	}

	if memTotalKB == 0 {
		return 0, 0, 0
	}

	total = memTotalKB * 1024
	available := max(memAvailKB*1024, total)
	used = total - available
	percent = float64(used) / float64(total) * 100
	return total, used, percent
}

func parseMeminfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func readDiskUsage(appdataDir string) (total, used, free uint64, percent float64) {
	path := appdataDir
	if path == "" {
		path = "/"
	}
	if _, err := os.Stat(path); err != nil {
		path = filepath.Dir(path)
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, 0
	}

	total = st.Blocks * uint64(st.Bsize)
	free = st.Bavail * uint64(st.Bsize)
	if free > total {
		free = total
	}
	used = total - free
	if total > 0 {
		percent = float64(used) / float64(total) * 100
	}
	return total, used, free, percent
}
