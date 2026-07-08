package discover

import (
	"bufio"
	"os"
	"os/user"
	"sort"
	"strconv"
	"strings"
)

// FirstHumanUser returns the first non-system, non-nobody user from
// /etc/passwd by scanning for UIDs in [minUID, maxUID]. Returns "" if
// nothing reasonable is found. Used to seed the prompt for the
// unprivileged user pev should drive renv/uv/pip checks under.
func FirstHumanUser() string {
	const minUID, maxUID = 1000, 65500
	type entry struct {
		name string
		uid  int
	}
	var entries []entry

	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Split(s.Text(), ":")
		if len(fields) < 7 {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid < minUID || uid > maxUID {
			continue
		}
		// Skip obvious service accounts that landed in the human range.
		shell := fields[6]
		if strings.HasSuffix(shell, "/nologin") || strings.HasSuffix(shell, "/false") {
			continue
		}
		entries = append(entries, entry{name: fields[0], uid: uid})
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].uid < entries[j].uid })
	return entries[0].name
}

// CurrentNonRootUser returns the current Unix username when the running
// process isn't root, or "" otherwise. Used by the assess command to
// auto-fill the unprivileged-user prompt.
func CurrentNonRootUser() string {
	if os.Geteuid() == 0 {
		return ""
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}
