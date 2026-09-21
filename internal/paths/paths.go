// Package paths resolves every file dshctl owns.
//
// All of dshctl's own state lives in one directory, so a single "rm -rf"
// removes it without touching anything DeepSeek Harness keeps elsewhere. The
// default is <harness home>/dshctl, where the harness home is $DSH_HOME or
// ~/.dsh.
//
// Every path accepted from the environment, the config file, or the command
// line is validated here rather than at the call site: a relative path silently
// moves state — and with it the operation lock — wherever the current directory
// happens to point, which is exactly the quiet failure this package exists to
// prevent. A leading "~" is expanded; "~user" is rejected rather than kept
// literal, because a literal "~user" directory is never what the operator meant.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Names and environment variables that locate dshctl's own files.
const (
	// StateDirName is dshctl's directory inside the DeepSeek Harness home.
	StateDirName = "dshctl"
	// HarnessDirName is the DeepSeek Harness home under the operating-system home.
	HarnessDirName = ".dsh"
	// EnvStateDir overrides the dshctl state directory.
	EnvStateDir = "DSHCTL_STATE_DIR"
	// EnvHarnessHome overrides the DeepSeek Harness home, as dsh itself does.
	EnvHarnessHome = "DSH_HOME"
	// EnvRepoDir overrides the managed checkout.
	EnvRepoDir = "DSH_REPO_DIR"
	// EnvPort overrides the Web server port.
	EnvPort = "DSH_PORT"
	// EnvNodeVersion overrides the preferred Node release.
	EnvNodeVersion = "DSH_NODE_VERSION"
	// EnvConfigFile overrides the config file location.
	EnvConfigFile = "DSHCTL_CONFIG"
	// EnvLang overrides the language operator-facing text is rendered in. It
	// outranks the shell's locale variables, so an operator can ask for English
	// on a Chinese machine and the other way around.
	EnvLang = "DSHCTL_LANG"
	// EnvLogFile overrides the log file location.
	EnvLogFile = "DSH_LOG_FILE"
)

// ErrNotAbsolute reports a path that would be resolved against the current
// directory of whichever process happens to run dshctl next.
var ErrNotAbsolute = errors.New("路径必须是绝对路径或 ~ 开头的路径")

// Getenv is the environment lookup every resolver takes, injected for tests.
type Getenv func(string) string

// Home returns the operating-system home directory.
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定用户主目录: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("无法确定用户主目录: 运行环境未提供")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("主目录不是绝对路径: %q", home)
	}
	return filepath.Clean(home), nil
}

// HarnessHome returns $DSH_HOME, or ~/.dsh when it is unset.
func HarnessHome(getenv Getenv) (string, error) {
	if raw := getenv(EnvHarnessHome); strings.TrimSpace(raw) != "" {
		return Resolve(raw)
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, HarnessDirName), nil
}

// StateDir returns $DSHCTL_STATE_DIR, or <harness home>/dshctl.
func StateDir(getenv Getenv) (string, error) {
	if raw := getenv(EnvStateDir); strings.TrimSpace(raw) != "" {
		return Resolve(raw)
	}
	root, err := HarnessHome(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, StateDirName), nil
}

// ConfigFile returns $DSHCTL_CONFIG, or <state dir>/config.json.
func ConfigFile(getenv Getenv) (string, error) {
	if raw := getenv(EnvConfigFile); strings.TrimSpace(raw) != "" {
		return Resolve(raw)
	}
	dir, err := StateDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Resolve expands a leading ~ or ~/ and requires an absolute result.
func Resolve(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrNotAbsolute
	}
	expanded, err := expandHome(trimmed)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		return "", fmt.Errorf("%w: %q", ErrNotAbsolute, raw)
	}
	return filepath.Clean(expanded), nil
}

// expandHome rewrites a leading "~" or "~/" using the operating-system home.
func expandHome(value string) (string, error) {
	if value == "~" {
		home, err := Home()
		if err != nil {
			return "", fmt.Errorf("无法展开 %q: %w", value, err)
		}
		return home, nil
	}
	if !strings.HasPrefix(value, "~/") && !strings.HasPrefix(value, `~\`) {
		return value, nil
	}
	home, err := Home()
	if err != nil {
		return "", fmt.Errorf("无法展开 %q: %w", value, err)
	}
	rest := strings.TrimLeft(value[1:], `/\`)
	if rest == "" {
		return home, nil
	}
	return filepath.Join(home, rest), nil
}

// Exists reports whether path exists, without following a final symlink.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsRegularFile reports whether path exists and is a regular file. A directory
// or a symlink where a file belongs is a configuration mistake worth reporting
// before something renames or truncates it.
func IsRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// EnsureDir creates dir and every missing parent with owner-only permissions.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("无法创建目录 %s: %w", dir, err)
	}
	return nil
}
