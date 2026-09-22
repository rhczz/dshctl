package history

import (
	"os"
	"testing"

	"github.com/rhczz/dshctl/internal/i18n"
)

// TestMain installs the catalog in the language the assertions are written in.
func TestMain(m *testing.M) {
	i18n.Use(i18n.New(i18n.ZH, Messages))
	os.Exit(m.Run())
}
