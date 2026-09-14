package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/paths"
)

// TestTheReadmeDocumentsEverySetting pins the promise the README makes to
// somebody who has to configure this tool without reading Go: every environment
// variable and every settings key is in it.
//
// It is a test rather than a convention because the failure mode is silent — a
// new variable that works and is documented nowhere is discovered by reading the
// source, and the person who needs the documentation most is the one who cannot.
func TestTheReadmeDocumentsEverySetting(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read the README: %v", err)
	}
	documented := string(readme)

	variables := []string{
		paths.EnvStateDir,
		paths.EnvHarnessHome,
		paths.EnvConfigFile,
		paths.EnvLogFile,
		paths.EnvRepoDir,
		paths.EnvPort,
		paths.EnvNodeVersion,
	}
	for _, name := range variables {
		if !strings.Contains(documented, name) {
			t.Errorf("环境变量 %s 没有被 README 记录", name)
		}
	}

	kind := reflect.TypeOf(config.File{})
	for index := 0; index < kind.NumField(); index++ {
		key := strings.Split(kind.Field(index).Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		if !strings.Contains(documented, "`"+key+"`") {
			t.Errorf("配置项 %s 没有被 README 记录", key)
		}
	}
}
