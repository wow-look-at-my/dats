package runner

import "strings"

// shellOptions is the prologue every dats command runs under. Without it a
// command that fails partway reports the exit code of whatever ran after it.
// A command that must tolerate a failure writes `|| true`.
const shellOptions = "set -euo pipefail"

// shellScript assembles what bash is asked to run. The cd belongs to the
// runner, which is why cmd may not contain one; emitting it here rather than
// setting the child's directory works for docker and ssh too, which re-enter a
// shell elsewhere.
func shellScript(cmd, workdir string) string {
	var b strings.Builder
	b.WriteString(shellOptions)
	b.WriteByte('\n')
	if workdir != "" {
		b.WriteString("cd ")
		b.WriteString(shellQuote(workdir))
		b.WriteByte('\n')
	}
	b.WriteString(cmd)
	return b.String()
}

// shellQuote renders s as a single-quoted shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
