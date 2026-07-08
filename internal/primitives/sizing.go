package primitives

import (
	"fmt"
	"strings"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("sizing", runSizing, []string{"cpus_min", "mem_gb_min", "disk_gb_min"})
}

// runSizing compares discovered host facts against thresholds. No shell-outs;
// values come from HostFacts populated during discovery.
//
// CPU and memory shortfalls are blocking (FAIL): the product will not run
// acceptably below those minimums. A disk shortfall is advisory (WARN) —
// disk is the easiest dimension to grow after the fact (attach a volume,
// resize the filesystem, point a data dir elsewhere), so an undersized disk
// is worth flagging without failing an otherwise-installable host. When both
// kinds are present FAIL wins (the blocking dimension takes the floor) but
// the disk shortfall is still named in the reason.
func runSizing(rc checks.RunCtx) checks.Result {
	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
	}
	var blocking, advisory []string

	if min, ok := getInt(rc.Check.With, "cpus_min"); ok && rc.Facts.CPUs < min {
		blocking = append(blocking, fmt.Sprintf("cpus=%d<min=%d", rc.Facts.CPUs, min))
	}
	if min, ok := getInt(rc.Check.With, "mem_gb_min"); ok {
		gotGB := rc.Facts.MemMB / 1024
		if gotGB < min {
			blocking = append(blocking, fmt.Sprintf("mem_gb=%d<min=%d", gotGB, min))
		}
	}
	if disk, ok := rc.Check.With["disk_gb_min"].(map[string]interface{}); ok {
		for mp, raw := range disk {
			min := 0
			switch v := raw.(type) {
			case int:
				min = v
			case int64:
				min = int(v)
			case float64:
				min = int(v)
			}
			got := rc.Facts.DiskGB[mp]
			if got < min {
				advisory = append(advisory, fmt.Sprintf("disk_gb[%s]=%d<min=%d", mp, got, min))
			}
		}
	}

	r.Evidence = []checks.Evidence{{
		Note: fmt.Sprintf("cpus=%d mem_mb=%d disk_gb=%v", rc.Facts.CPUs, rc.Facts.MemMB, rc.Facts.DiskGB),
	}}

	// CPU/memory shortfall is blocking. Surface any disk shortfall alongside
	// it so the SE sees the full picture, but the status stays FAIL.
	if len(blocking) > 0 {
		r.Status = checks.StatusFail
		r.Reason = strings.Join(append(blocking, advisory...), "; ")
		return r
	}
	// Disk-only shortfall: advisory. Installable as-is, but note it.
	if len(advisory) > 0 {
		r.Status = checks.StatusWarn
		r.Reason = strings.Join(advisory, "; ") + " (disk is advisory — grow the volume before going to production)"
		return r
	}
	r.Status = checks.StatusPass
	return r
}
