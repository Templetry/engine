package verify

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Templetry/engine/internal/fakedocker"
)

func TestMain(m *testing.M) {
	fakedocker.Main()
	os.Exit(m.Run())
}

func TestDockerArgsKeepTheRunLineWhole(t *testing.T) {
	// The run line is one argument to sh -ec, never split by the engine:
	// quoting, pipes and && are the shell's business inside the image.
	run := `echo "a b" | tr a-z A-Z && test -f package.json`
	args := dockerArgs("alpine:3", run, "/w")
	if args[len(args)-1] != run {
		t.Errorf("run line was split: %q", args)
	}
	if args[len(args)-3] != "sh" || args[len(args)-2] != "-ec" {
		t.Errorf("run line must go through sh -ec: %q", args)
	}
}

func TestRunMountsTheDirectoryAndStreamsOutput(t *testing.T) {
	logPath := fakedocker.Install(t, 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run("node:22", "npm ci", dir, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	call := fakedocker.Last(t, logPath)
	abs, _ := filepath.Abs(dir)
	want := dockerArgs("node:22", "npm ci", abs)
	if strings.Join(call.Args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("docker args = %q, want %q", call.Args, want)
	}
	if len(call.Files) != 1 || call.Files[0] != "package.json" {
		t.Errorf("container saw %v, want the rendered project", call.Files)
	}
	if stdout.String() != "fake docker: stdout\n" || stderr.String() != "fake docker: stderr\n" {
		t.Errorf("output not streamed: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunResolvesRelativeDirectories(t *testing.T) {
	logPath := fakedocker.Install(t, 0)
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(parent)
	if err := Run("img", "true", "proj", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	call := fakedocker.Last(t, logPath)
	mount := call.Args[3]
	if !filepath.IsAbs(mount[:strings.LastIndex(mount, ":")]) || !strings.HasSuffix(mount, "proj:/work") {
		t.Errorf("docker needs an absolute host path, got %q", mount)
	}
}

func TestRunReportsAFailingContainer(t *testing.T) {
	fakedocker.Install(t, 3)
	var stderr bytes.Buffer
	err := Run("img", "false", t.TempDir(), &bytes.Buffer{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "verify failed") || !strings.Contains(err.Error(), "3") {
		t.Errorf("err = %v, want a verify failure carrying the exit status", err)
	}
	if !strings.Contains(stderr.String(), "fake docker: stderr") {
		t.Errorf("the container's stderr must reach the caller even on failure: %q", stderr.String())
	}
}

func TestRunWithoutDocker(t *testing.T) {
	fakedocker.Hide(t)
	err := Run("img", "true", t.TempDir(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "docker not found") {
		t.Errorf("err = %v, want a clear 'docker not found'", err)
	}
}
