package nodejs

import (
	"strings"
	"testing"
)

// The gate is the one place a resolved runtime is judged, so it is also the one
// place the minimum lives. The rows below are the G section of the decision
// table; matrix_completeness_test.go holds the frozen list.

// The releases the gate is exercised with. They are literals on purpose: the
// gate must be judged against values a test states, not against whatever the
// configuration constants happen to say.
const (
	gateMinimum = "24.12.0"
	gateTested  = "24.20.0"
)

// gateRows is the G section of the decision table: one case per row.
var gateRows = map[string]func(*testing.T){
	"G1": testGateG1RefusesAReleaseBelowTheMinimum,
	"G2": testGateG2AcceptsTheMinimumItself,
	"G3": testGateG3AcceptsTheVerifiedRelease,
	"G4": testGateG4AcceptsANewerPatchOfTheVerifiedMajor,
	"G5": testGateG5WarnsAboutAnUnverifiedMajor,
	"G6": testGateG6RefusesAnOlderMajor,
	"G7": testGateG7IgnoresPreReleaseAndBuildSuffixes,
}

// TestGateMatrix runs every row of the gate table.
func TestGateMatrix(t *testing.T) {
	for id, run := range gateRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// installationAt builds the installation the gate rows judge. The origin is
// PATH so the reason text has something concrete to name.
func installationAt(version string) Installation {
	return Installation{
		Version:  version,
		NodePath: "/usr/local/bin/node",
		BinDir:   "/usr/local/bin",
		Source:   SourcePath,
	}
}

// testGateG1RefusesAReleaseBelowTheMinimum pins the hard floor. It is refused
// whatever named it — configuration, --node, the environment or PATH — because
// dshctl cannot serve the Web client with it.
func testGateG1RefusesAReleaseBelowTheMinimum(t *testing.T) {
	verdict := Assess(installationAt("24.11.9"), gateMinimum, gateTested)
	if verdict.Status != TooOld {
		t.Fatalf("Status = %q, want %q", verdict.Status, TooOld)
	}
	if !strings.Contains(verdict.Reason, "24.11.9") || !strings.Contains(verdict.Reason, gateMinimum) {
		t.Fatalf("Reason = %q, want it to name the release and the minimum", verdict.Reason)
	}
	if verdict.Remedy == "" {
		t.Fatal("a refused release must carry the remedy block")
	}
}

// testGateG2AcceptsTheMinimumItself pins the boundary from below: the floor is
// inclusive, and the row above it (G1) pins that the release just under it is
// not.
func testGateG2AcceptsTheMinimumItself(t *testing.T) {
	verdict := Assess(installationAt(gateMinimum), gateMinimum, gateTested)
	if verdict.Status != Supported {
		t.Fatalf("Status = %q, want %q", verdict.Status, Supported)
	}
	if verdict.Reason != "" || verdict.Remedy != "" {
		t.Fatalf("verdict = %+v, want no reason and no remedy for a supported release", verdict)
	}
}

// testGateG3AcceptsTheVerifiedRelease pins the ordinary case.
func testGateG3AcceptsTheVerifiedRelease(t *testing.T) {
	if got := Assess(installationAt(gateTested), gateMinimum, gateTested).Status; got != Supported {
		t.Fatalf("Status = %q, want %q", got, Supported)
	}
}

// testGateG4AcceptsANewerPatchOfTheVerifiedMajor pins that staying inside the
// verified major version is enough: dshctl is verified against a release, not
// against one exact build.
func testGateG4AcceptsANewerPatchOfTheVerifiedMajor(t *testing.T) {
	if got := Assess(installationAt("24.99.1"), gateMinimum, gateTested).Status; got != Supported {
		t.Fatalf("Status = %q, want %q", got, Supported)
	}
}

// testGateG5WarnsAboutAnUnverifiedMajor pins the middle ground: a release the
// Web client's module scan has never been exercised against is used, but said
// out loud, because the failure it can cause is visible only in the browser.
func testGateG5WarnsAboutAnUnverifiedMajor(t *testing.T) {
	for _, version := range []string{"25.0.0", "26.3.0"} {
		verdict := Assess(installationAt(version), gateMinimum, gateTested)
		if verdict.Status != Untested {
			t.Fatalf("Assess(%q) = %q, want %q", version, verdict.Status, Untested)
		}
		if !strings.Contains(verdict.Reason, version) || !strings.Contains(verdict.Reason, gateTested) {
			t.Fatalf("Reason = %q, want it to name the release and the verified one", verdict.Reason)
		}
		if verdict.Remedy != "" {
			t.Fatalf("Remedy = %q, want none: an unverified release is not refused", verdict.Remedy)
		}
	}
}

// testGateG6RefusesAnOlderMajor pins that the floor is a version comparison and
// not a major-version comparison: 23.x is below 24.12.0 and is refused.
func testGateG6RefusesAnOlderMajor(t *testing.T) {
	verdict := Assess(installationAt("23.9.9"), gateMinimum, gateTested)
	if verdict.Status != TooOld {
		t.Fatalf("Status = %q, want %q", verdict.Status, TooOld)
	}
	if verdict.Remedy == "" {
		t.Fatal("a refused release must carry the remedy block")
	}
}

// testGateG7IgnoresPreReleaseAndBuildSuffixes pins that a suffix does not move a
// release out of its major version: 24.20.0-rc.1 is still 24.20.
func testGateG7IgnoresPreReleaseAndBuildSuffixes(t *testing.T) {
	for _, version := range []string{"24.20.0-rc.1", "24.20.0+meta", "v24.20.0"} {
		if got := Assess(installationAt(version), gateMinimum, gateTested).Status; got != Supported {
			t.Fatalf("Assess(%q) = %q, want %q", version, got, Supported)
		}
	}
}

// TestGateReasonNamesTheInstallation pins what the warning and the refusal have
// to contain to be actionable: which binary, which release, and where it came
// from.
func TestGateReasonNamesTheInstallation(t *testing.T) {
	installation := Installation{
		Version:  "22.14.0",
		NodePath: "/opt/homebrew/bin/node",
		BinDir:   "/opt/homebrew/bin",
		Source:   SourcePath,
	}
	reason := Assess(installation, gateMinimum, gateTested).Reason
	for _, want := range []string{"22.14.0", "/opt/homebrew/bin/node", "PATH", gateMinimum} {
		if !strings.Contains(reason, want) {
			t.Fatalf("Reason = %q, want it to contain %q", reason, want)
		}
	}

	// A version-manager origin is reported as such rather than as a path.
	managed := Installation{Version: "22.14.0", NodePath: "/home/u/.nvm/versions/node/v22.14.0/bin/node", Source: SourceNVM}
	if reason := Assess(managed, gateMinimum, gateTested).Reason; !strings.Contains(reason, "nvm") {
		t.Fatalf("Reason = %q, want it to name the origin", reason)
	}
}

// TestGateIgnoresAnUnknownRelease pins the consequence of resolving a release
// from a directory name: nothing validates it first, so the gate has to answer
// for a value it cannot read. An unreadable release is below every floor.
func TestGateIgnoresAnUnknownRelease(t *testing.T) {
	if got := Assess(installationAt(""), gateMinimum, gateTested).Status; got != TooOld {
		t.Fatalf("Status = %q, want %q for a release that could not be read", got, TooOld)
	}
}

// TestRemediesNamesEveryWayToInstall pins the remedy block, line by line.
//
// The block is the part of a failure an operator acts on, and the whole reason
// the failure is allowed to be fatal. A version manager missing from it is a
// user for whom dshctl reports a problem and no way out, so every documented
// installation route is asserted here rather than summarised as "the message
// mentions nvm".
func TestRemediesNamesEveryWayToInstall(t *testing.T) {
	got := Remedies(gateMinimum, gateTested)
	wants := []string{
		"nvm install 24",
		"fnm install 24",
		"brew install node@24",
		"n 24",
		"volta install node@24",
		"asdf install nodejs " + gateTested,
		"mise use -g node@24",
		"nodenv install " + gateTested,
		"https://nodejs.org/en/download",
		"--node",
		"DSH_NODE_VERSION",
		"or name it once: --node <version> or DSH_NODE_VERSION=<version>",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("Remedies() =\n%s\nwant it to contain %q", got, want)
		}
	}
}

// TestDescribeExplainsARequestedRelease pins the text an operator reads when the
// release they configured is installed nowhere: the request, what each place had
// instead, and the way out.
func TestDescribeExplainsARequestedRelease(t *testing.T) {
	failure := &Failure{
		Requested: "24.99.0",
		Observations: []Observation{
			{Source: SourceManagers, Detail: "the newest is 24.19.0"},
			{Source: SourcePath, Path: "/usr/bin/node", Version: "22.14.0"},
		},
	}
	got := Describe(failure, gateMinimum, gateTested)
	for _, want := range []string{
		"Node 24.99.0 was not found",
		"was not found (looked in the nvm/fnm install roots and on PATH)",
		"PATH",
		"the newest is 24.19.0",
		"/usr/bin/node is 22.14.0",
		"nvm install 24",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe() =\n%s\nwant it to contain %q", got, want)
		}
	}
}

// TestDescribeExplainsADiscoveryFailure pins the other half: nothing was asked
// for, so the failure is about the environment, and a broken binary is named
// with the reason it could not be used.
func TestDescribeExplainsADiscoveryFailure(t *testing.T) {
	missing := Describe(&Failure{Observations: []Observation{{Source: SourcePath, Detail: "no node"}}}, gateMinimum, gateTested)
	if !strings.Contains(missing, "no usable node") || !strings.Contains(missing, "PATH: no node") {
		t.Fatalf("Describe() =\n%s\nwant the PATH to be named as empty", missing)
	}
	if !strings.Contains(missing, "brew install node@24") {
		t.Fatalf("Describe() =\n%s\nwant the remedy block", missing)
	}

	broken := Describe(&Failure{
		Observations: []Observation{{Source: SourcePath, Path: "/usr/bin/node", Detail: "could not be executed: exit status 1"}},
		Err:          errFailedProbe,
	}, gateMinimum, gateTested)
	for _, want := range []string{"/usr/bin/node", "could not be executed", "exit status 1"} {
		if !strings.Contains(broken, want) {
			t.Errorf("Describe() =\n%s\nwant it to contain %q", broken, want)
		}
	}
}

// errFailedProbe is the fictional probe failure the describe rows carry.
var errFailedProbe = probeFailure("exit status 1")

// probeFailure is a stand-in for the error a failed probe reports.
type probeFailure string

// Error implements error.
func (p probeFailure) Error() string { return string(p) }

// FuzzGateAgreesWithCompare pins the property the gate must never violate: it is
// a restatement of the version comparison, so a release below the minimum is
// refused and a release inside the verified major version is accepted, whatever
// strings reach it. The reader of a release accepts garbage, so the gate has to
// answer for garbage too rather than panicking or inventing a status.
func FuzzGateAgreesWithCompare(f *testing.F) {
	for _, version := range []string{"", "0", "v", "24", "24.12.0", "24.20.0", "24.20.0-rc.1", "garbage", "26.1.0"} {
		f.Add(version, gateMinimum, gateTested)
	}
	f.Fuzz(func(t *testing.T, version, minimum, tested string) {
		verdict := Assess(installationAt(version), minimum, tested)
		switch verdict.Status {
		case Supported, Untested, TooOld:
		default:
			t.Fatalf("Assess(%q, %q, %q) = %q, want a documented status", version, minimum, tested, verdict.Status)
		}
		if Compare(version, minimum) < 0 && verdict.Status != TooOld {
			t.Fatalf("Assess(%q, %q, %q) = %q, want %q: the minimum is a floor",
				version, minimum, tested, verdict.Status, TooOld)
		}
		if Compare(version, minimum) >= 0 && majorOf(version) == majorOf(tested) && verdict.Status != Supported {
			t.Fatalf("Assess(%q, %q, %q) = %q, want %q: inside the verified major version",
				version, minimum, tested, verdict.Status, Supported)
		}
		if verdict.Status == TooOld && verdict.Remedy == "" {
			t.Fatalf("Assess(%q, %q, %q) refused a release without a remedy", version, minimum, tested)
		}
		if verdict.Status == Supported && (verdict.Reason != "" || verdict.Remedy != "") {
			t.Fatalf("Assess(%q, %q, %q) = %+v, want no reason for a supported release", version, minimum, tested, verdict)
		}
	})
}
