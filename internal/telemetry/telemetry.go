// Package telemetry collects a worker node's live resource picture — load,
// memory, disk, slot usage, and the set of Docker images it has cached — for the
// control plane's fleet view. All collection is best-effort and dependency-free;
// metrics unavailable on a platform are simply reported as zero.
package telemetry

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Image is one cached Docker image.
type Image struct {
	Repo   string `json:"repo"`    // "repository:tag"
	SizeMB int64  `json:"size_mb"` // on-disk size, best effort
}

// Snapshot is a point-in-time view of a node.
type Snapshot struct {
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	CPUs        int     `json:"cpus"`
	MemTotalMB  int64   `json:"mem_total_mb"`
	MemFreeMB   int64   `json:"mem_free_mb"`
	DiskFreeMB  int64   `json:"disk_free_mb"`
	Slots       int     `json:"slots"`
	Running     int     `json:"running"`
	Images      []Image `json:"images"`
	CollectedAt string  `json:"collected_at"`
}

// Collect gathers host metrics for the given work directory and combines them
// with the caller-supplied slot/running counts and cached image list.
func Collect(workDir string, slots, running int, images []Image) Snapshot {
	l1, l5, l15 := loadAvg()
	memTotal, memFree := memInfo()
	return Snapshot{
		Load1: l1, Load5: l5, Load15: l15,
		CPUs:       runtime.NumCPU(),
		MemTotalMB: memTotal, MemFreeMB: memFree,
		DiskFreeMB:  diskFreeMB(workDir),
		Slots:       slots,
		Running:     running,
		Images:      images,
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// loadAvg reads /proc/loadavg (Linux). Returns zeros elsewhere.
func loadAvg() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(f[0], 64)
	l5, _ = strconv.ParseFloat(f[1], 64)
	l15, _ = strconv.ParseFloat(f[2], 64)
	return l1, l5, l15
}

// memInfo reads /proc/meminfo (Linux). Returns zeros elsewhere.
func memInfo() (totalMB, freeMB int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		kb, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			totalMB = kb / 1024
		case "MemAvailable:":
			freeMB = kb / 1024
		}
	}
	return totalMB, freeMB
}

// diskFreeMB returns free space (MB) on the filesystem holding path.
func diskFreeMB(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize) / (1024 * 1024))
}

// DockerImages lists images cached on this node via the docker CLI.
func DockerImages(ctx context.Context, docker string) []Image {
	if docker == "" {
		docker = "docker"
	}
	out, err := exec.CommandContext(ctx, docker, "images",
		"--format", "{{.Repository}}:{{.Tag}}\t{{.Size}}").Output()
	if err != nil {
		return nil
	}
	var images []Image
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 2)
		if len(parts) != 2 || strings.HasPrefix(parts[0], "<none>") {
			continue
		}
		images = append(images, Image{Repo: parts[0], SizeMB: parseSize(parts[1])})
	}
	return images
}

// parseSize converts docker's human size ("123MB", "1.2GB", "900kB") to MB.
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "GB"):
		mult, s = 1024, strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult, s = 1, strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "kB"):
		mult, s = 1.0/1024, strings.TrimSuffix(s, "kB")
	case strings.HasSuffix(s, "B"):
		mult, s = 1.0/(1024*1024), strings.TrimSuffix(s, "B")
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(v * mult)
}
