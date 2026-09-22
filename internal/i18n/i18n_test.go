package i18n

import (
	"testing"
)

// Msg is a message id the mechanism tests render through.
const Msg = "state.text"

func env(values map[string]string) Getenv {
	return func(name string) string { return values[name] }
}

// TestResolveReadsTheLanguageFromTheEnvironment pins the order and the fallback:
// an explicit choice wins, then the shell's locale variables, and anything
// unrecognized is English rather than nothing.
func TestResolveReadsTheLanguageFromTheEnvironment(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]string
		want   Lang
	}{
		{"nothing set", nil, EN},
		{"explicit chinese", map[string]string{"DSHCTL_LANG": "zh"}, ZH},
		{"explicit english", map[string]string{"DSHCTL_LANG": "en"}, EN},
		{"locale chinese simplified", map[string]string{"LANG": "zh_CN.UTF-8"}, ZH},
		{"locale chinese traditional", map[string]string{"LANG": "zh_TW.UTF-8"}, ZH},
		{"locale chinese tag", map[string]string{"LANG": "zh-Hans"}, ZH},
		{"locale english", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"messages beats lang", map[string]string{"LC_MESSAGES": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"}, ZH},
		{"lc_all beats messages", map[string]string{"LC_ALL": "en_US.UTF-8", "LC_MESSAGES": "zh_CN.UTF-8"}, EN},
		{"explicit beats every locale", map[string]string{"DSHCTL_LANG": "zh", "LC_ALL": "en_US.UTF-8"}, ZH},
		{"c locale is english", map[string]string{"LANG": "C"}, EN},
		{"posix locale is english", map[string]string{"LC_ALL": "POSIX"}, EN},
		{"unknown language falls back", map[string]string{"LANG": "fr_FR.UTF-8"}, EN},
		{"blank values are unset", map[string]string{"DSHCTL_LANG": "  ", "LANG": "zh_CN.UTF-8"}, ZH},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Resolve(env(testCase.values)); got != testCase.want {
				t.Fatalf("Resolve = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestMessageRendersInEveryLanguage(t *testing.T) {
	message := Message{EN: "port %d is held", ZH: "端口 %d 被占用"}
	if got := message.In(EN, 3080); got != "port 3080 is held" {
		t.Errorf("EN = %q", got)
	}
	if got := message.In(ZH, 3080); got != "端口 3080 被占用" {
		t.Errorf("ZH = %q", got)
	}
}

// TestMessageFallsBackToEnglish pins that a gap in one language shows the
// operator the other one instead of an empty line.
func TestMessageFallsBackToEnglish(t *testing.T) {
	message := Message{EN: "only english"}
	if got := message.In(ZH); got != "only english" {
		t.Errorf("ZH fallback = %q", got)
	}
}

// TestUnknownIDRendersAsItself pins the failure mode: a typo shows up in the
// output and in a golden file rather than as an empty line nobody can explain.
func TestUnknownIDRendersAsItself(t *testing.T) {
	translator := New(EN, Catalog{})
	if got := translator.T("state.nowhere"); got != "state.nowhere" {
		t.Fatalf("unknown id = %q", got)
	}
	if translator.Has("state.nowhere") {
		t.Fatal("Has accepted an id the catalog does not carry")
	}
}

func TestTranslatorUsesTheRequestedLanguage(t *testing.T) {
	translator := New(ZH, Catalog{Msg: {EN: "text", ZH: "文字"}})
	if got := translator.T(Msg); got != "文字" {
		t.Fatalf("ZH = %q", got)
	}
	translator = New(EN, Catalog{Msg: {EN: "text", ZH: "文字"}})
	if got := translator.T(Msg); got != "text" {
		t.Fatalf("EN = %q", got)
	}
}

// TestMergeRefusesADuplicateID pins the constraint that keeps two layers from
// silently hiding each other's text.
func TestMergeRefusesADuplicateID(t *testing.T) {
	first := Catalog{"state.x": {EN: "one", ZH: "一"}}
	second := Catalog{"state.x": {EN: "two", ZH: "二"}}
	if _, err := Merge(first, second); err == nil {
		t.Fatal("a duplicate id was merged")
	}
	merged, err := Merge(first, Catalog{"cli.y": {EN: "two", ZH: "二"}})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged = %d messages, want 2", len(merged))
	}
}

// TestAuditFindsAnIncompleteMessage pins the audit a catalog's own test runs, so
// a missing translation is a red test rather than a fallback found in
// production.
func TestAuditFindsAnIncompleteMessage(t *testing.T) {
	catalog := Catalog{
		"state.ok":      {EN: "ok", ZH: "好"},
		"state.missing": {EN: "only english"},
	}
	incomplete := catalog.Audit()
	if len(incomplete) != 1 || incomplete[0] != "state.missing" {
		t.Fatalf("Audit = %v, want [state.missing]", incomplete)
	}
}
