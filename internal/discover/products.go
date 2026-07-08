package discover

import (
	"path/filepath"

	"github.com/posit-dev/pev/internal/system"
)

// Products records which Posit professional products appear installed on the host.
type Products struct {
	Workbench      bool `json:"workbench"`
	Connect        bool `json:"connect"`
	PackageManager bool `json:"package_manager"`
}

// Candidate server/license-manager binaries, relative to the filesystem root.
// Connect's server binary was renamed rstudio-connect -> connect, so both names
// are accepted; the per-product license-manager path is stable across releases
// and is the primary anchor.
var (
	workbenchPaths = []string{"usr/lib/rstudio-server/bin/rserver"}
	connectPaths   = []string{
		"opt/rstudio-connect/bin/license-manager",
		"opt/rstudio-connect/bin/connect",
		"opt/rstudio-connect/bin/rstudio-connect",
	}
	packageManagerPaths = []string{
		"opt/rstudio-pm/bin/license-manager",
		"opt/rstudio-pm/bin/rstudio-pm",
	}
)

// DetectProducts probes by binary presence. rstudio-server on $PATH signals
// Workbench; otherwise detection falls through to the on-disk binary paths.
func DetectProducts() Products {
	p := detectProductsAt("/", system.FileExists)
	if system.CommandExists("rstudio-server") {
		p.Workbench = true
	}
	return p
}

// detectProductsAt resolves the candidate paths against root and reports which
// products appear installed. Split out from DetectProducts so tests can point
// it at a temp dir; the $PATH-based Workbench probe stays in DetectProducts.
func detectProductsAt(root string, exists func(string) bool) Products {
	any := func(rels []string) bool {
		for _, rel := range rels {
			if exists(filepath.Join(root, rel)) {
				return true
			}
		}
		return false
	}
	return Products{
		Workbench:      any(workbenchPaths),
		Connect:        any(connectPaths),
		PackageManager: any(packageManagerPaths),
	}
}

// SelectedFromFlag merges user flag input with discovery: if the flag list is
// empty, return whatever was detected; if the user named explicit products,
// honor that.
func SelectedFromFlag(flagList []string, p Products) []string {
	if len(flagList) > 0 {
		return flagList
	}
	out := []string{}
	if p.Workbench {
		out = append(out, "workbench")
	}
	if p.Connect {
		out = append(out, "connect")
	}
	if p.PackageManager {
		out = append(out, "packagemanager")
	}
	return out
}
