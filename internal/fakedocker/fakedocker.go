// Package fakedocker puts a stand-in "docker" on PATH for tests: a copy
// of the running test binary that, when started under that name, records
// its arguments and the contents of the mounted directory, prints a line
// on each stream and exits with the status the test asked for.
//
// A test package opts in from TestMain:
//
//	func TestMain(m *testing.M) { fakedocker.Main(); os.Exit(m.Run()) }
package fakedocker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const (
	envLog  = "TEMPLETRY_FAKE_DOCKER_LOG"
	envExit = "TEMPLETRY_FAKE_DOCKER_EXIT"
)

// Call is what one docker invocation saw.
type Call struct {
	Args  []string `json:"args"`
	Files []string `json:"files"` // files under the -v mount, slash-separated
}

// Main turns the process into the fake docker when it was started as one.
// It returns normally otherwise.
func Main() {
	logPath := os.Getenv(envLog)
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if logPath == "" || name != "docker" {
		return
	}
	call := Call{Args: os.Args[1:]}
	for i, a := range call.Args {
		if a == "-v" && i+1 < len(call.Args) {
			mount := call.Args[i+1]
			host := mount[:strings.LastIndex(mount, ":")]
			_ = filepath.WalkDir(host, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					rel, _ := filepath.Rel(host, p)
					call.Files = append(call.Files, filepath.ToSlash(rel))
				}
				return nil
			})
		}
	}
	data, _ := json.Marshal(call)
	_ = os.WriteFile(logPath, data, 0o644)
	fmt.Fprintln(os.Stdout, "fake docker: stdout")
	fmt.Fprintln(os.Stderr, "fake docker: stderr")
	code, _ := strconv.Atoi(os.Getenv(envExit))
	os.Exit(code)
}

// Install puts the fake docker first on PATH for the rest of the test and
// makes it exit with exitCode. Read the invocation back with Last.
func Install(t *testing.T, exitCode int) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	name := "docker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	copyFile(t, self, filepath.Join(bin, name))
	logPath := filepath.Join(t.TempDir(), "docker-call.json")
	t.Setenv("PATH", bin)
	t.Setenv(envLog, logPath)
	t.Setenv(envExit, strconv.Itoa(exitCode))
	return logPath
}

// Hide leaves PATH without any docker for the rest of the test.
func Hide(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// Last reads the invocation the fake docker recorded.
func Last(t *testing.T, logPath string) Call {
	t.Helper()
	var c Call
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("docker was not called: %v", err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o755); err != nil {
		t.Fatal(err)
	}
}
