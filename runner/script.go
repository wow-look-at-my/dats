package runner

import "strings"

// shellOptions is the prologue every dats command runs under.
//
// A test command is an assertion about what happened, so a command that fails
// halfway must not report the exit code of whatever ran after it. errexit ends
// the command at the line that failed, nounset ends it on a name nothing set
// (an expansion that quietly produced nothing is the same lie in a different
// place), and pipefail carries a failure out of the left of a pipe. A command
// that must tolerate a failure says so with `|| true`, where the reader sees it.
const shellOptions = "set -euo pipefail"

// shellScript assembles what bash is asked to run: the options, the cd into the
// workdir when the file named one, then the author's command.
//
// The cd belongs to the runner, which is why cmd may not contain one. Emitting
// it here rather than setting the child process's directory keeps one mechanism
// for every backend, including the ones that re-enter a shell somewhere else:
// docker and ssh.
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

// shellQuote renders s as one single-quoted shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
