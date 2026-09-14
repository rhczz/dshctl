package logfile

import (
	"errors"
	"io/fs"
	"regexp"
)

// maxMatchBytes bounds how much of a log is searched for a pattern. The address
// the server announced is written near the end of the file, and the runtime
// record keeps it afterwards, so the window only has to cover a recent start.
const maxMatchBytes = 8 << 20

// AllMatches returns every first-group match of pattern in the trailing bytes of
// a file, oldest first.
//
// Returns:
//   - the captured values.
//   - whether the scan window began mid-file, so an older match exists outside
//     it. A caller that needs "there is no such line" to be a fact must check
//     this: inside a window, absence is only absence from the window.
//   - an error when the file cannot be read; a missing file yields no matches.
func AllMatches(path string, pattern *regexp.Regexp) ([]string, bool, error) {
	data, truncated, err := readTailBytes(path, maxMatchBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	found := pattern.FindAllSubmatch(data, -1)
	values := make([]string, 0, len(found))
	for _, match := range found {
		if len(match) < 2 {
			continue
		}
		values = append(values, string(match[1]))
	}
	return values, truncated, nil
}
