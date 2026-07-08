package discover

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectProductsAt locks in detection against the real on-disk binary
// names. The regression that prompted this: current Connect ships its server
// binary as /opt/rstudio-connect/bin/connect (renamed from rstudio-connect),
// so the old single-path probe reported Connect as absent on every modern
// install. Detection now anchors on the stable per-product license-manager
// binary and accepts both Connect server-binary names.
func TestDetectProductsAt(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  Products
	}{
		{"nothing installed", nil, Products{}},
		{
			"connect via renamed server binary (connect)",
			[]string{"opt/rstudio-connect/bin/connect"},
			Products{Connect: true},
		},
		{
			"connect via license-manager",
			[]string{"opt/rstudio-connect/bin/license-manager"},
			Products{Connect: true},
		},
		{
			"connect via legacy server binary (rstudio-connect)",
			[]string{"opt/rstudio-connect/bin/rstudio-connect"},
			Products{Connect: true},
		},
		{
			"workbench via on-disk rserver",
			[]string{"usr/lib/rstudio-server/bin/rserver"},
			Products{Workbench: true},
		},
		{
			"ppm via rstudio-pm",
			[]string{"opt/rstudio-pm/bin/rstudio-pm"},
			Products{PackageManager: true},
		},
		{
			"ppm via license-manager",
			[]string{"opt/rstudio-pm/bin/license-manager"},
			Products{PackageManager: true},
		},
		{
			"all three present",
			[]string{
				"usr/lib/rstudio-server/bin/rserver",
				"opt/rstudio-connect/bin/connect",
				"opt/rstudio-pm/bin/rstudio-pm",
			},
			Products{Workbench: true, Connect: true, PackageManager: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, rel := range tc.files {
				full := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("x"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := detectProductsAt(root, fileExistsRegular); got != tc.want {
				t.Fatalf("detectProductsAt = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// fileExistsRegular mirrors system.FileExists (regular-file check) for tests,
// avoiding a dependency edge from this package's test onto internal/system.
func fileExistsRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// TestPPMLinuxSlug locks in the OS→PPM-binary-slug mapping, and in
// particular the empty-string result for any OS pev does not recognize.
// That empty result is load-bearing: it is exactly why the built-in renv
// check (checks/common/50-languages.yaml) was moved off the binary
// `__linux__/<slug>/` repo URL and onto the source repo. If a future edit
// makes ppmLinuxSlug return a non-empty guess for an unknown OS, the renv
// regression Ralf hit on Pop!_OS (a malformed `__linux__//latest` URL) would
// silently come back for any pack still interpolating this slug.
func TestPPMLinuxSlug(t *testing.T) {
	cases := []struct {
		osID string
		want string
	}{
		{"ubuntu-22.04", "jammy"},
		{"ubuntu-24.04", "noble"},
		{"rhel-8", "centos8"},
		{"rhel-9", "rhel9"},
		{"rhel-10", "rhel10"},
		// Unrecognized / derivative / unknown OSes have no PPM binary slug.
		{"unknown", ""},
		{"", ""},
		{"ubuntu-20.04", ""}, // EOS Ubuntu: not in the supported map
		{"ubuntu-26.04", ""}, // not yet added
		{"rhel-7", ""},       // out of the supported window
		{"pop", ""},          // Pop!_OS normalizes to "unknown" upstream; defensive
	}
	for _, tc := range cases {
		t.Run(tc.osID, func(t *testing.T) {
			if got := ppmLinuxSlug(tc.osID); got != tc.want {
				t.Fatalf("ppmLinuxSlug(%q) = %q, want %q", tc.osID, got, tc.want)
			}
		})
	}
}
