package output

import (
	"io"
	"strings"
	"testing"
)

type sample struct {
	Port    int    `json:"port"`
	State   string `json:"state"`
	private string
}

func TestResultWritesTheOperatorsResult(t *testing.T) {
	var out, errs strings.Builder
	reporter := New(&out, &errs)
	reporter.Result("状态: 运行中")
	reporter.Resultf("端口 %d", 3080)
	if got := out.String(); got != "状态: 运行中\n端口 3080\n" {
		t.Errorf("stdout = %q", got)
	}
	if errs.String() != "" {
		t.Errorf("stderr = %q, want nothing", errs.String())
	}
}

func TestWarnfIsFramedAndGoesToStandardError(t *testing.T) {
	var out, errs strings.Builder
	New(&out, &errs).Warnf("无法获取远程更新: %v", io.EOF)
	if got := errs.String(); got != "警告: 无法获取远程更新: EOF\n" {
		t.Errorf("stderr = %q", got)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want nothing", out.String())
	}
}

func TestErrorfCarriesTheCallersOwnFrame(t *testing.T) {
	var out, errs strings.Builder
	New(&out, &errs).Errorf("错误: %s失败: %v", "更新", io.EOF)
	if got := errs.String(); got != "错误: 更新失败: EOF\n" {
		t.Errorf("stderr = %q", got)
	}
}

func TestJSONIsIndentedTheWayScriptsParse(t *testing.T) {
	var out, errs strings.Builder
	value := sample{Port: 3080, State: "running", private: "hidden"}
	if err := New(&out, &errs).JSON(value); err != nil {
		t.Fatalf("json: %v", err)
	}
	want := "{\n  \"port\": 3080,\n  \"state\": \"running\"\n}\n"
	if got := out.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestJSONReportsAnUnrenderableValue(t *testing.T) {
	var out, errs strings.Builder
	if err := New(&out, &errs).JSON(func() {}); err == nil {
		t.Fatal("a value that cannot be rendered was accepted")
	}
}
