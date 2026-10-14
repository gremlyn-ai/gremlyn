//go:build linux

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	environHelperEnv    = "GREMLYN_ENVIRON_HELPER"
	environHelperSecret = "GREMLYN_ENVIRON_SECRET"
)

func TestEnvironHelperProcess(t *testing.T) {
	mode := os.Getenv(environHelperEnv)
	if mode == "" {
		t.Skip("only runs as a helper process")
	}
	if mode == "protect" {
		if err := protectProcessEnviron(); err != nil {
			fmt.Println("PROTECT-FAILED")
			os.Exit(0)
		}
	}
	out, err := exec.Command("cat", fmt.Sprintf("/proc/%d/environ", os.Getpid())).Output()
	switch {
	case err != nil:
		fmt.Println("DENIED")
	case strings.Contains(string(out), os.Getenv(environHelperSecret)):
		fmt.Println("READ")
	default:
		fmt.Println("UNKNOWN")
	}
	os.Exit(0)
}

func runEnvironHelper(t *testing.T, mode, secret string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestEnvironHelperProcess$")
	cmd.Env = append(os.Environ(), environHelperEnv+"="+mode, environHelperSecret+"="+secret)
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestProtectProcessEnviron_ChildCannotReadParentEnviron(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("needs cat")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads any process's environment, dumpable or not")
	}
	secret := "environ-" + uuid.NewString()
	assert.Contains(t, runEnvironHelper(t, "open", secret), "READ",
		"control: without the call, a same-user child reads its parent's environment")
	assert.Contains(t, runEnvironHelper(t, "protect", secret), "DENIED",
		"after PR_SET_DUMPABLE 0, a same-user child cannot read it")
}
