package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// forceHostGOOS points the path spelling at the named host for this test.
func forceHostGOOS(t *testing.T, goos string) {
	t.Helper()
	previous := hostGOOS
	hostGOOS = goos
	t.Cleanup(func() { hostGOOS = previous })
}

func TestHostCommandPathLeavesAPosixHostAlone(t *testing.T) {
	forceHostGOOS(t, "linux")
	assert.Equal(t, "/tmp/dats-x/test-1/inputs/a.txt", hostCommandPath("/tmp/dats-x/test-1/inputs/a.txt"))
}

// The conversion answers the HOST, never the separator this binary compiled
// for: this case passes on a posix builder, which is where an APE is built.
func TestHostCommandPathGivesAnNTHostForwardSlashes(t *testing.T) {
	forceHostGOOS(t, "windows")
	assert.Equal(t, "C:/Users/RUNNER/AppData/Local/Temp/dats-x/test-1/inputs/a.txt",
		hostCommandPath(`C:\Users\RUNNER\AppData\Local\Temp\dats-x\test-1\inputs\a.txt`))
}

func TestCommandPathConvertsWhenNoSSHTargetIsSet(t *testing.T) {
	forceHostGOOS(t, "windows")
	ctx := &TestContext{BaseDir: `C:\t\dats-x`, TestIndex: 1}
	assert.Equal(t, "C:/t/dats-x/test-1/outputs/out.txt", ctx.commandPath(`C:\t\dats-x\test-1\outputs\out.txt`))
}

func TestJoinFixturePathConvertsARawNTDirectory(t *testing.T) {
	forceHostGOOS(t, "windows")
	assert.Equal(t, "C:/t/dats-x/shared/config.json", joinFixturePath(`C:\t\dats-x\shared`, "config.json"))
	assert.Equal(t, "C:/t/dats-x/shared/sub/config.json", joinFixturePath(`C:\t\dats-x\shared`, `sub\config.json`))
}

func TestExpandPlaceholdersSpellsAnNTPathForBash(t *testing.T) {
	forceHostGOOS(t, "windows")
	ctx := &TestContext{
		BaseDir:    `C:\t\dats-x`,
		TestIndex:  1,
		InputPaths: map[string]string{"go.mod": `C:\t\dats-x\test-1\inputs\go.mod`},
		OutputsDir: `C:\t\dats-x\test-1\outputs`,
		SharedDir:  `C:\t\dats-x\shared`,
	}
	got := ExpandPlaceholders("{shared.gt.exe} {inputs.go.mod} > {outputs.log.txt}", ctx)
	assert.Equal(t, "C:/t/dats-x/shared/gt.exe C:/t/dats-x/test-1/inputs/go.mod > C:/t/dats-x/test-1/outputs/log.txt", got)
}
