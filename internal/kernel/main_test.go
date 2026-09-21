package kernel

import (
	"os"
	"testing"

	"github.com/rhczz/dshctl/internal/i18n"
)

// TestMain installs the catalog in the language the assertions are written in.
//
// These tests pin the text an operator reads, and that text is Chinese in
// v0.2.5's rendering. Installing it explicitly is the same choice the
// conformance oracle makes: the language of a run is a property of the run, not
// of the machine the suite happens to execute on.
func TestMain(m *testing.M) {
	i18n.Use(i18n.New(i18n.ZH, Messages))
	os.Exit(m.Run())
}
