// Package primitives implements the executable units that YAML checks dispatch
// to. Every primitive registers itself in checks.Register() at init() time.
// Adding a primitive: add a file here, register with allowed `with:` keys,
// document in docs/primitives.md, and add a positive + negative test.
package primitives

import (
	"fmt"
	"strconv"
	"time"

	"github.com/posit-dev/pev/internal/checks"
)

// getTimeout reads the conventional `timeout_seconds` knob and returns
// it as a time.Duration; falls back to def when the field is absent or
// non-positive. Centralized so every primitive has the same template-
// expansion behavior (string → int via getInt) and bounded-default
// shape rather than handrolling the dance.
func getTimeout(m map[string]interface{}, def time.Duration) time.Duration {
	if t, ok := getInt(m, "timeout_seconds"); ok && t > 0 {
		return time.Duration(t) * time.Second
	}
	return def
}

// unknownf returns a StatusUnknown Result with a formatted reason. Used when
// a primitive can't decide because its `with:` payload is malformed at runtime
// (lint catches static cases at load time).
func unknownf(c checks.Check, format string, a ...interface{}) checks.Result {
	return checks.Result{
		ID: c.ID, Title: c.Title,
		Status: checks.StatusUnknown, Reason: fmt.Sprintf(format, a...),
	}
}

func getString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getInt(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		if x == "" {
			return 0, false
		}
		// Allow YAML authors to write `port: "{{ .Inputs.postgres_port }}"`
		// — the templater renders to a string at expand time, so the
		// primitive has to reparse. Empty string is treated as missing
		// so the primitive can SKIP cleanly.
		n, err := strconv.Atoi(x)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func getBool(m map[string]interface{}, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func getStringSlice(m map[string]interface{}, key string) ([]string, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	switch x := v.(type) {
	case []string:
		return x, true
	case []interface{}:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func getIntSlice(m map[string]interface{}, key string) ([]int, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	if xs, ok := v.([]interface{}); ok {
		out := make([]int, 0, len(xs))
		for _, e := range xs {
			switch n := e.(type) {
			case int:
				out = append(out, n)
			case int64:
				out = append(out, int(n))
			case float64:
				out = append(out, int(n))
			}
		}
		return out, true
	}
	return nil, false
}
