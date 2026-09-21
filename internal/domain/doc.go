// Package domain is the model of what dshctl manages: instances of the Web
// server, the position a checkout can be moved to, and the rules that decide
// which state belongs to whom.
//
// It is the innermost layer. Everything here is pure: no file, no process, no
// clock, no command line, which is why the rules of the lifecycle can be read
// and tested without a machine, and why every other layer may depend on this one
// while this one depends on nobody.
//
// The layer it does not contain is policy: which instances a command covers,
// when a lock is taken, what a message says. That lives in the kernel and above
// it, and it changes per front-end in a way the model does not.
package domain
