// Package i18n renders operator-facing text in the language the machine asks
// for, with English as the answer whenever the machine does not say.
//
// Three things make it an extension point rather than a lookup table.
//
//  1. Completeness is a type, not a habit. A Message carries one field per
//     supported language, so adding a language is adding a field: the compiler
//     then names every message that has not been translated yet, which is the
//     only kind of translation check that cannot be forgotten.
//  2. The language is resolved in one place, from the same variables a Unix
//     shell exports, and anything unrecognized falls back to English instead of
//     to a half-translated screen.
//  3. The texts themselves do not live here. This package is the capability —
//     resolution, the catalog type, merging several layers' catalogs, and the
//     audit that finds an incomplete one — while each layer owns the words it
//     speaks: the kernel carries what it does, a front-end carries what only
//     that front-end can say, and the shell merges them at startup. That is what
//     lets a second front-end add messages without touching the framework.
package i18n

import (
	"fmt"
	"strings"
)

// Lang is a language this build speaks.
type Lang string

const (
	// EN is the default: the language a machine falls back to when nothing says
	// otherwise, and the one every message must have.
	EN Lang = "en"
	// ZH is Chinese, in the form the operator-facing text has always used.
	ZH Lang = "zh"
)

// Message is one operator-facing text in every supported language.
//
// Adding a language means adding a field here and to every message: the
// compiler enumerates the work, so a message can never ship half-translated in
// silence.
type Message struct {
	EN string
	ZH string
}

// In renders the message in one language, substituting arguments the way
// fmt.Sprintf does.
//
// A message without text in the requested language falls back to English rather
// than showing an empty line: a missing translation is a gap to fill, not a
// reason to show the operator nothing.
func (m Message) In(lang Lang, args ...any) string {
	format := m.EN
	if lang == ZH && m.ZH != "" {
		format = m.ZH
	}
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// Getenv is the environment lookup the resolver uses.
type Getenv func(string) string

// Resolve answers which language to use.
//
// The order is: an explicit DSHCTL_LANG first, then the variables a shell
// exports for a locale (LC_ALL, LC_MESSAGES, LANG), then English. A locale names
// a language, a region and an encoding ("zh_CN.UTF-8"); only the language part
// decides, and a language this build does not speak resolves to English.
func Resolve(getenv Getenv) Lang {
	for _, name := range []string{"DSHCTL_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := strings.TrimSpace(getenv(name)); value != "" {
			return parseLang(value)
		}
	}
	return EN
}

// parseLang reads the language part of a locale or language tag.
func parseLang(value string) Lang {
	// "zh_CN.UTF-8" -> "zh_CN" -> "zh"; "en-US" -> "en".
	base := value
	if index := strings.IndexAny(base, ".@"); index >= 0 {
		base = base[:index]
	}
	base = strings.ReplaceAll(base, "-", "_")
	if index := strings.Index(base, "_"); index >= 0 {
		base = base[:index]
	}
	switch strings.ToLower(base) {
	case "zh":
		return ZH
	case "en":
		return EN
	}
	// "C" and "POSIX" are the absence of a locale, not a language.
	return EN
}

// Translator renders the catalog's messages in one language.
type Translator struct {
	lang    Lang
	catalog Catalog
}

// New returns a translator for one language.
func New(lang Lang, catalog Catalog) *Translator {
	if lang != ZH {
		lang = EN
	}
	return &Translator{lang: lang, catalog: catalog}
}

// Lang reports the language this translator renders in.
func (t *Translator) Lang() Lang { return t.lang }

// T renders one message.
//
// An unknown id renders as the id itself. That is deliberate: it shows up in the
// output and in a golden file as an obvious marker rather than as an empty line,
// so a typo is found by the next run instead of by a reader who cannot tell what
// was meant.
func (t *Translator) T(id string, args ...any) string {
	message, ok := t.catalog[id]
	if !ok {
		return id
	}
	return message.In(t.lang, args...)
}

// Has reports whether the catalog carries an id, so a test can ask the question
// the renderer answers by returning the id.
func (t *Translator) Has(id string) bool {
	_, ok := t.catalog[id]
	return ok
}

// current is the process-wide translator.
//
// It is set once, by the shell, before any command runs: the language is a
// property of the invocation, not of a function's arguments, and threading it
// through every call site would put a parameter in signatures that have nothing
// else to do with language.
var current = New(EN, Catalog{})

// Use installs the translator every T call renders through.
func Use(translator *Translator) { current = translator }

// Current reports the installed translator.
func Current() *Translator { return current }

// T renders one message through the installed translator.
func T(id string, args ...any) string { return current.T(id, args...) }
