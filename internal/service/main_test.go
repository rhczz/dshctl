package service

import (
	"os"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/i18n"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/repo"
)

// TestMain installs the catalog in the language the assertions are written in.
//
// These tests pin the text an operator reads, and that text is Chinese in
// v0.2.5's rendering. Installing it explicitly is the same choice the
// conformance oracle makes: the language of a run is a property of the run, not
// of the machine the suite happens to execute on.
func TestMain(m *testing.M) {
	// The catalog the binary installs: every layer's words, merged. A test that
	// renders a lower layer's error needs that layer's catalog too.
	catalog, err := i18n.Merge(Messages, config.Messages, nodejs.Messages, repo.Messages)
	if err != nil {
		panic(err)
	}
	i18n.Use(i18n.New(i18n.ZH, catalog))
	os.Exit(m.Run())
}
