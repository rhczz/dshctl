package domain

import "fmt"

// Latest names the remote tip: the one selector that always means "wherever
// origin/master is now". Every other target is a fixed position.
const Latest = "latest"

// Target is a resolved version to move a checkout to.
type Target struct {
	// Commit is the full revision to move to.
	Commit string
	// Selector is what the operator asked for, recorded verbatim.
	Selector string
	// Name names the target in messages, empty when the selector is only an
	// abbreviation of the commit.
	Name string
	// Latest marks the remote tip: the switch returns to the master branch and
	// fast-forwards instead of detaching.
	Latest bool
}

// Label names the target in a message: the short commit, and the selector only
// when it says something the commit does not.
func (t Target) Label() string {
	if t.Name == "" {
		return ShortCommit(t.Commit)
	}
	return fmt.Sprintf("%s（%s）", ShortCommit(t.Commit), t.Name)
}

// ShortCommit is how a revision is named in a message.
func ShortCommit(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
