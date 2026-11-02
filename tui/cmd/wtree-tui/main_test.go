package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAnswersForTheScript(t *testing.T) {
	dir := t.TempDir()
	write := func(name, version string) string {
		path := filepath.Join(dir, name)
		body := "#!/bin/sh\necho '" + version + "'\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Setenv("WTREE_SCRIPT", write("good", `{"v":1,"script":"0.4.0","contract":1}`))
	if code := run([]string{"--check"}); code != 0 {
		t.Fatalf("known contract: %d", code)
	}
	t.Setenv("WTREE_SCRIPT", write("new", `{"v":1,"script":"9.0.0","contract":99}`))
	if code := run([]string{"--check"}); code == 0 {
		t.Fatal("an unknown contract passed")
	}
	t.Setenv("WTREE_SCRIPT", filepath.Join(dir, "missing"))
	if code := run([]string{"--check"}); code == 0 {
		t.Fatal("a missing script passed")
	}
}

// Captured, as in cd $(wtree --tui), the screen would go into the capture, and the terminal would
// stay blank and take keys. It says so and ends.
func TestCapturedStdoutIsRefused(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "wtree")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho '{\"v\":1,\"script\":\"0.4.0\",\"contract\":1}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WTREE_SCRIPT", script)
	t.Setenv("HOME", dir)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	if code := run(nil); code != 2 {
		t.Fatalf("exit %d", code)
	}
}
