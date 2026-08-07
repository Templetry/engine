package verify

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
)

// dockerArgs builds the docker invocation: the directory mounted at /work,
// the manifest's run line executed by sh inside the image.
func dockerArgs(image, run, absDir string) []string {
	return []string{"run", "--rm",
		"-v", absDir + ":/work",
		"-w", "/work",
		image, "sh", "-ec", run}
}

// Run executes the manifest's verify command (ADR-0004) in a Docker
// container with dir mounted at /work. Command output streams to the given
// writers; a non-zero container exit is the error.
func Run(image, run, dir string, stdout, stderr io.Writer) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("verify needs Docker on the host (docker not found in PATH)")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	cmd := exec.Command("docker", dockerArgs(image, run, abs)...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("verify failed: %w", err)
	}
	return nil
}
