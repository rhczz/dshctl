package service

import "github.com/rhczz/dshctl/internal/logfile"

// This file is the product side of the log format.
//
// The mechanism (internal/logfile) knows how to write, rotate, tail and parse a
// sectioned log; it does not know what the sections are called or who writes
// them. Those are this product's values, and they live with the layer that owns
// the log contract: the operator reads these markers, and `dshctl logs --build`
// looks for them by name.

// logFormat is how dshctl fences its sections.
//
// The marker is a promise to everybody who has ever read this log, so it is
// spelled out here rather than derived: `===== 2026-02-03 04:05:06 dshctl build =====`.
var logFormat = logfile.Format{
	Prefix:  "=====",
	Product: "dshctl",
	Layout:  "2006-01-02 15:04:05",
}

// Section titles dshctl writes and reads back.
const (
	sectionStart    = "start"
	sectionBuild    = "build"
	sectionUpdate   = "update"
	sectionRollback = "rollback"
)
