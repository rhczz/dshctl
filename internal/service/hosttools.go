package service

import "github.com/rhczz/dshctl/internal/host"

// This file is the product side of the probe inventory.
//
// The host package knows how to read the dialects (internal/host); which tools
// this product trusts, and in what order, is its own decision — and one it
// states here rather than hiding inside the mechanism. lsof comes first because
// it names the owning process; ss is the same on Linux; netstat is the last
// resort that may only say "something is listening". ps reads process facts.
var hostTools = host.Tools{
	Port:    []string{"lsof", "ss", "netstat"},
	Process: "ps",
}
