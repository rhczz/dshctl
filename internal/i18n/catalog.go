package i18n

import "fmt"

// Catalog is a set of operator-facing texts, keyed by a stable id.
//
// The type is the capability; the texts are not. A catalog belongs to the layer
// that speaks: the kernel carries the words for what it does, a front-end
// carries the words only that front-end can say, and the shell merges them at
// startup. Keeping the texts out of this package is what lets a second front-end
// add its own without editing the framework.
type Catalog map[string]Message

// Merge combines catalogs into the one the process renders through.
//
// A duplicate id is an error rather than a silent override: two layers claiming
// the same id means one of them is about to be invisible, and which one wins
// would depend on merge order. Ids are namespaced by their owner instead
// (state.*, cli.*, http.*), and this is what enforces that.
func Merge(catalogs ...Catalog) (Catalog, error) {
	merged := Catalog{}
	for _, catalog := range catalogs {
		for id, message := range catalog {
			if _, exists := merged[id]; exists {
				return nil, fmt.Errorf("duplicate message id: %s", id)
			}
			merged[id] = message
		}
	}
	return merged, nil
}

// Audit reports the ids that are incomplete: a message missing a language would
// render as a fallback, which is a gap for whoever writes the translations
// rather than something a reader should discover. Each catalog's own test calls
// it.
func (c Catalog) Audit() []string {
	incomplete := []string{}
	for id, message := range c {
		if message.EN == "" || message.ZH == "" {
			incomplete = append(incomplete, id)
		}
	}
	return incomplete
}
