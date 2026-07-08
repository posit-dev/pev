package operatingsystem

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNormalize covers the main matrix: Ubuntu 22/24, RHEL 8/9/10, and the
// RHEL-family rebuilds that must collapse onto the same canonical id.
func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		osr  map[string]string
		want string
	}{
		{"ubuntu-22", map[string]string{"ID": "ubuntu", "VERSION_ID": "22.04", "PRETTY_NAME": "Ubuntu 22.04.5 LTS"}, "ubuntu-22.04"},
		{"ubuntu-24", map[string]string{"ID": "ubuntu", "VERSION_ID": "24.04", "PRETTY_NAME": "Ubuntu 24.04.2 LTS"}, "ubuntu-24.04"},
		{"rhel-8", map[string]string{"ID": "rhel", "VERSION_ID": "8.10"}, "rhel-8"},
		{"rhel-9", map[string]string{"ID": "rhel", "VERSION_ID": "9.4"}, "rhel-9"},
		{"rhel-10", map[string]string{"ID": "rhel", "VERSION_ID": "10.0"}, "rhel-10"},
		{"alma-9", map[string]string{"ID": "almalinux", "VERSION_ID": "9.4"}, "rhel-9"},
		{"alma-10", map[string]string{"ID": "almalinux", "VERSION_ID": "10.0"}, "rhel-10"},
		{"rocky-9", map[string]string{"ID": "rocky", "VERSION_ID": "9.5"}, "rhel-9"},
		// CentOS normalizes to rhel-<major> here so the RHEL-family catalog
		// applies. This is intentionally NOT a statement of support: the
		// os.supported check re-reads the raw ID and rejects CentOS Stream
		// 9/10 (upstream of RHEL, not a binary-compatible rebuild). The
		// normalize/support split is by design — see the package doc.
		{"centos-stream-10", map[string]string{"ID": "centos", "VERSION_ID": "10"}, "rhel-10"},
		{"oracle-9", map[string]string{"ID": "ol", "VERSION_ID": "9.4"}, "rhel-9"},
		{"unknown-distro", map[string]string{"ID": "exotic", "VERSION_ID": "1"}, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize(tc.osr).ID
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReadOSRelease ensures the parser handles quoted values and comments.
func TestReadOSRelease(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "os-release")
	body := "# header\nID=almalinux\nVERSION_ID=\"9.4\"\nPRETTY_NAME=\"AlmaLinux 9.4 (Teal Serval)\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, ok := readOSRelease(p)
	if !ok {
		t.Fatal("readOSRelease returned !ok for valid file")
	}
	if m["ID"] != "almalinux" || m["VERSION_ID"] != "9.4" {
		t.Fatalf("parse mismatch: %+v", m)
	}
}
