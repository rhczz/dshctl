package conformance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// stubSource is the probe tools the scenarios see. Whether a port is free is a
// property of the machine, not of dshctl, so the tools that answer that question
// are replaced by ones that always answer "nothing is listening". The fail-closed
// path — no probe tool at all — keeps its own scenarios with an empty PATH.
const stubSource = `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	switch name {
	case "lsof":
		// The real lsof exits 1 with no output when nothing matches.
		os.Exit(1)
	case "ss":
		fmt.Println("State  Recv-Q Send-Q Local Address:Port Peer Address:Port")
	case "netstat":
		fmt.Println("Active Internet connections (only servers)")
		fmt.Println("Proto Recv-Q Send-Q Local Address           Foreign Address         State")
	case "ps":
		os.Exit(1)
	case "node":
		version("v24.20.0")
	case "pnpm":
		version("9.0.0")
	}
}

// version answers the probe dshctl makes and stays quiet for anything else.
func version(release string) {
	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--version" {
			fmt.Println(release)
			return
		}
	}
}
`

// stubTools are the names the stub is installed under for every scenario: the
// probes whose answer would otherwise be a property of the machine.
var stubTools = []string{"lsof", "ss", "netstat", "ps"}

// buildStubDir compiles the stub once and installs it under every probe name.
func buildStubDir(root string) (string, error) {
	dir := filepath.Join(root, "stubs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	source := filepath.Join(dir, "stub.go")
	if err := os.WriteFile(source, []byte(stubSource), 0o644); err != nil {
		return "", err
	}
	binary := filepath.Join(dir, "stub")
	if err := buildStub(source, binary); err != nil {
		return "", err
	}
	for _, name := range stubTools {
		if err := copyFile(binary, filepath.Join(dir, name+exeSuffix())); err != nil {
			return "", err
		}
	}
	if err := os.Remove(binary); err != nil {
		return "", err
	}
	return dir, nil
}

func buildStub(source, out string) error {
	build := exec.Command("go", "build", "-o", out, source)
	build.Env = buildEnv()
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("building the stub: %w\n%s", err, output)
	}
	return nil
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o755)
}
