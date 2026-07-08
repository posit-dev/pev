package discover

import (
	"bufio"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// readCPUs returns logical CPU count via runtime, with /proc/cpuinfo as fallback.
func readCPUs() int {
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "processor") {
			count++
		}
	}
	return count
}

// readMemMB returns total memory in MB by parsing /proc/meminfo.
func readMemMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}

// readDiskFreeGB returns free disk on the filesystem containing path, in GB.
// Bavail/Bsize widths vary by platform (uint32 on some, int64 on others), so
// route through math/big-style guarded conversions to keep gosec G115 happy.
func readDiskFreeGB(path string) int {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	// On linux/amd64 Bavail is already uint64; on linux/386 it is uint32. The
	// conversions are required for portability even though they may be
	// no-ops on a given target — silence both unconvert and gosec.
	bavail := uint64(st.Bavail) //nolint:gosec,unconvert // platform-portable widening
	bsize := uint64(st.Bsize)   //nolint:gosec,unconvert // platform-portable widening
	gb := (bavail * bsize) / (1024 * 1024 * 1024)
	if gb > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(gb)
}
