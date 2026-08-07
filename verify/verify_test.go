package verify

import (
	"strings"
	"testing"
)

func TestDockerArgs(t *testing.T) {
	args := dockerArgs("node:22", "npm ci && npm run build", "/abs/out")
	got := strings.Join(args, " ")
	want := "run --rm -v /abs/out:/work -w /work node:22 sh -ec npm ci && npm run build"
	if got != want {
		t.Errorf("dockerArgs:\n got %q\nwant %q", got, want)
	}
}
