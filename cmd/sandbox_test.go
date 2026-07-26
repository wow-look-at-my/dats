package cmd

// Tests for the sandbox flags: the default is a sandbox, opting out is
// explicit, and contradicting yourself is an error rather than a silent
// precedence rule.

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/runner"
)

// newSandboxProbe returns a fresh command with the sandbox flags registered
// exactly as the root command registers them, whose RunE records the resolved
// configuration. A fresh instance per case keeps pflag's Changed state from
// bleeding between tests.
func newSandboxProbe(cfg **runner.SandboxConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "probe",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveSandbox(cmd.Flags())
			if err != nil {
				return err
			}
			*cfg = resolved
			return nil
		},
	}
	registerSandboxFlags(cmd.PersistentFlags())
	return cmd
}

func TestSandboxFlagResolution(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantMode  runner.SandboxMode // "" = expect no sandbox at all
		wantImage string
		wantErr   string
	}{
		{
			name:      "absent means auto -- sandboxing is the default",
			args:      nil,
			wantMode:  runner.SandboxAuto,
			wantImage: runner.DefaultSandboxImage,
		},
		{name: "--sandbox=bwrap", args: []string{"--sandbox=bwrap"}, wantMode: runner.SandboxBwrap, wantImage: runner.DefaultSandboxImage},
		{name: "--sandbox=docker", args: []string{"--sandbox=docker"}, wantMode: runner.SandboxDocker, wantImage: runner.DefaultSandboxImage},
		{
			name:      "--sandbox-image overrides the default image",
			args:      []string{"--sandbox=docker", "--sandbox-image=alpine:3.20"},
			wantMode:  runner.SandboxDocker,
			wantImage: "alpine:3.20",
		},
		{name: "--sandbox=none opts out", args: []string{"--sandbox=none"}},
		{name: "--no-sandbox opts out", args: []string{"--no-sandbox"}},
		{name: "--no-sandbox with --sandbox=none agrees", args: []string{"--no-sandbox", "--sandbox=none"}},
		{
			name:    "--no-sandbox contradicting --sandbox is an error",
			args:    []string{"--no-sandbox", "--sandbox=bwrap"},
			wantErr: "--no-sandbox conflicts with --sandbox=bwrap",
		},
		{name: "unknown backend is an error", args: []string{"--sandbox=firejail"}, wantErr: "--sandbox: unknown sandbox mode"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg *runner.SandboxConfig
			cmd := newSandboxProbe(&cfg)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()

			if tc.wantErr != "" {
				require.NotNil(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.Nil(t, err)
			if tc.wantMode == "" {
				assert.Nil(t, cfg, "opting out must produce no sandbox configuration")
				return
			}
			require.NotNil(t, cfg)
			assert.Equal(t, tc.wantMode, cfg.Mode)
			assert.Equal(t, tc.wantImage, cfg.Image)
		})
	}
}

func TestRunTestsWithoutSandboxRunsOnHost(t *testing.T) {
	// The nil configuration every library caller (and every opted-out run)
	// gets must not require a backend or change the output.
	datsFile := writeDats(t, "host.dats", `
tests:
  - desc: runs
    cmd: echo hi
    outputs:
      stdout:
        - hi
`)
	var out bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{datsFile}, &out, 0, nil))
	assert.NotContains(t, out.String(), "# sandbox:")
}
