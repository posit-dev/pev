// Package discover performs read-only probes that build up the HostFacts
// snapshot used to gate checks and seed prompts.
package discover

import (
	"context"
	"os"
	"runtime"

	"github.com/posit-dev/pev/internal/operatingsystem"
	"github.com/posit-dev/pev/internal/system"
)

// HostFacts is the structured snapshot of the host pev assesses.
type HostFacts struct {
	Hostname string         `json:"hostname"`
	FQDN     string         `json:"fqdn,omitempty"`
	OS       string         `json:"os"`        // canonical id, e.g. ubuntu-22.04
	OSPretty string         `json:"os_pretty"` // human pretty name
	OSFamily string         `json:"os_family"` // ubuntu | rhel | unknown
	Arch     string         `json:"arch"`      // amd64 | arm64
	CPUs     int            `json:"cpus"`
	MemMB    int            `json:"mem_mb"`
	DiskGB   map[string]int `json:"disk_gb"` // mountpoint -> free GB
	Root     bool           `json:"root"`
	Products Products       `json:"products"`
	R        []string       `json:"r_versions,omitempty"`
	Python   []string       `json:"python_versions,omitempty"`
	Quarto   []string       `json:"quarto_versions,omitempty"`

	// Tooling — set by Gather based on availability on the running host.
	// Used by the catalog's `applies_to.requires` fact gate so the engine
	// can SKIP language-dependent checks rather than running them and
	// reporting unhelpful UNKNOWNs.
	HasUV  bool `json:"has_uv,omitempty"`
	HasPip bool `json:"has_pip,omitempty"`
	HasApt bool `json:"has_apt,omitempty"`
	HasDNF bool `json:"has_dnf,omitempty"`

	// LatestR / LatestPython are the highest-versioned versioned
	// installations found under /opt/R/<ver>/bin/R and
	// /opt/python/<ver>/bin/python(3). Empty when none are detected.
	// Used by the user-install checks so renv/uv/pip use the most
	// recent toolchain a customer would deploy with.
	LatestR      string `json:"latest_r,omitempty"`
	LatestPython string `json:"latest_python,omitempty"`

	// PPMLinuxSlug is the `__linux__` distro slug Posit Package Manager
	// uses in binary repo URLs (jammy, noble, rhel9, etc). Computed from OS
	// at Gather time so YAML packs can write
	// `https://packagemanager.posit.co/cran/__linux__/{{ .Facts.PPMLinuxSlug }}/latest`
	// and target the right binary repo per host.
	//
	// IMPORTANT: this is the empty string on any OS pev does not recognize
	// (Pop!_OS and other derivatives, unknown distros) — PPM only publishes
	// binaries for the handful of distros in ppmLinuxSlug. A pack that
	// interpolates this into a URL without an applies_to.os gate will emit a
	// malformed `__linux__//latest` URL on those hosts. The built-in renv
	// check deliberately uses the source repo (no slug) so it stays portable
	// across every host; reserve the binary slug for OS-gated packs.
	PPMLinuxSlug string `json:"ppm_linux_slug,omitempty"`
}

// Gather runs every discovery probe and returns the resulting HostFacts.
// Probes that fail emit a warning to the log but never abort.
func Gather(ctx context.Context) HostFacts {
	osd := operatingsystem.Detect()
	hostname, _ := os.Hostname()

	hf := HostFacts{
		Hostname: hostname,
		FQDN:     resolveFQDN(ctx, hostname),
		OS:       osd.ID,
		OSPretty: osd.Pretty,
		OSFamily: osd.Family,
		Arch:     runtime.GOARCH,
		Root:     system.IsRoot(),
		DiskGB:   map[string]int{},
	}

	hf.CPUs = readCPUs()
	hf.MemMB = readMemMB()
	hf.DiskGB["/"] = readDiskFreeGB("/")
	hf.Products = DetectProducts()
	hf.R = ScanRVersions()
	hf.Python = ScanPythonVersions()
	hf.Quarto = ScanQuartoVersions()
	hf.LatestR = LatestVersionedPath(hf.R)
	hf.LatestPython = LatestVersionedPath(hf.Python)
	hf.HasUV = system.CommandExists("uv")
	hf.HasPip = system.CommandExists("pip3") || system.CommandExists("pip")
	hf.HasApt = system.CommandExists("apt-get")
	hf.HasDNF = system.CommandExists("dnf")
	hf.PPMLinuxSlug = ppmLinuxSlug(hf.OS)
	return hf
}

// ppmLinuxSlug maps pev's canonical OS id to the `__linux__/<slug>` PPM
// path component. Slugs come from
// https://packagemanager.posit.co/client/#/repos/cran/setup — they are NOT
// /etc/os-release IDs, so we keep the mapping explicit.
func ppmLinuxSlug(osID string) string {
	switch osID {
	case "ubuntu-22.04":
		return "jammy"
	case "ubuntu-24.04":
		return "noble"
	case "rhel-8":
		return "centos8"
	case "rhel-9":
		return "rhel9"
	case "rhel-10":
		return "rhel10"
	}
	return ""
}
