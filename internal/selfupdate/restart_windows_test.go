//go:build windows

package selfupdate

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRestartWaitsAndPreservesArguments(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	installed := filepath.Join(dir, "新版本 sysbox.exe")
	staged := filepath.Join(dir, "update.exe")
	for _, path := range []string{installed, staged} {
		if err := os.WriteFile(path, binary, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceExecutable(staged, installed); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(dir, "result.json")
	args := []string{installed, "-test.run=^TestRestartHelperProcess$", "--", "--theme", "light", "含 空格参数"}
	env := append(os.Environ(), "SYSBOX_RESTART_TEST=success", "SYSBOX_RESTART_RESULT="+result)
	if err := Restart(installed, args, env); err != nil {
		t.Fatal(err)
	}
	// 返回时子进程必须已写完结果；只调用 Start 会过早返回。
	data, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("重启返回前新进程应已完成：%v", err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("重启参数发生变化：%q，期望 %q", got, args)
	}
}

func TestRestartReportsChildFailure(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{exe, "-test.run=^TestRestartHelperProcess$"}
	env := append(os.Environ(), "SYSBOX_RESTART_TEST=failure")
	err = Restart(exe, args, env)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("应返回新进程的退出错误，实际为 %v", err)
	}
}

func TestRestartReportsMissingExecutable(t *testing.T) {
	if err := Restart(filepath.Join(t.TempDir(), "missing.exe"), nil, os.Environ()); err == nil {
		t.Fatal("重启路径不存在时应返回错误")
	}
}

func TestRestartHelperProcess(t *testing.T) {
	switch os.Getenv("SYSBOX_RESTART_TEST") {
	case "success":
		data, err := json.Marshal(os.Args)
		if err != nil {
			os.Exit(24)
		}
		if err := os.WriteFile(os.Getenv("SYSBOX_RESTART_RESULT"), data, 0o600); err != nil {
			os.Exit(25)
		}
		os.Exit(0)
	case "failure":
		os.Exit(23)
	}
}
