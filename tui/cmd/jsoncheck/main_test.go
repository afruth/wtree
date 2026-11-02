package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		input string
		want  int
		says  string
	}{
		{"good", nil, "{\"a\":1}\n[1]\n7\n", 0, ""},
		{"no input", nil, "", 0, ""},
		{"no newline at the end", nil, `{"a":1}`, 0, ""},
		{"not json", nil, "{\"a\":1}\noops\n", 1, "stdin:2:"},
		{"cut", nil, `{"a":`, 1, "stdin:1:"},
		{"not utf-8", nil, "\"a\xffb\"\n", 1, "UTF-8"},
		{"two values on a line", nil, "{} {}\n", 1, "stdin:1:"},
		{"an event", []string{"-events"}, `{"v":1,"event":"end","ms":1}` + "\n", 0, ""},
		{"an event of a newer script", []string{"-events"}, `{"v":1,"event":"future"}` + "\n", 0, ""},
		{"not an event of this contract", []string{"-events"}, `{"v":2,"event":"end"}` + "\n", 1, "contract"},
		{"a bad flag", []string{"-nosuch"}, "", 2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := run(c.args, strings.NewReader(c.input), &stderr); got != c.want {
				t.Fatalf("exit %d, want %d: %s", got, c.want, stderr.String())
			}
			if !strings.Contains(stderr.String(), c.says) {
				t.Fatalf("stderr: %s", stderr.String())
			}
		})
	}
}

func TestRunFiles(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(good, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("oops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if got := run([]string{good}, nil, &stderr); got != 0 {
		t.Fatalf("exit %d", got)
	}
	if got := run([]string{good, bad}, nil, &stderr); got != 1 {
		t.Fatalf("exit %d", got)
	}
	if got := run([]string{bad, filepath.Join(dir, "nosuch")}, nil, &stderr); got != 2 {
		t.Fatalf("exit %d", got)
	}
}

func TestEveryRecordingIsGood(t *testing.T) {
	files, err := filepath.Glob("../../internal/core/testdata/*.ndjson")
	if err != nil || len(files) == 0 {
		t.Fatalf("%v, %d files", err, len(files))
	}
	var stderr bytes.Buffer
	if got := run(append([]string{"-events"}, files...), nil, &stderr); got != 0 {
		t.Fatalf("exit %d: %s", got, stderr.String())
	}
}
