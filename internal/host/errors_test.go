package host

import "errors"

// errTestLookup is the failure a missing executable reports.
var errTestLookup = errors.New("executable file not found in $PATH")
