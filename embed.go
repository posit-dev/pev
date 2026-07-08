package main

import "embed"

//go:embed all:checks
var embeddedChecks embed.FS

const embeddedRoot = "checks"
