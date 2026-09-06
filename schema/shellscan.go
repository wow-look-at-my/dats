package schema

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// A dats command is one command, not a shell script. Everything this file
// rejects has a schema key that does the same job where the runner can see it:
// a redirect hides from dats exactly the output dats exists to assert on, a
// separator turns one test into several whose failures the exit code hides,
// and a cd makes the command's meaning depend on where the reader started.
//
// The check runs on a real parse (mvdan.cc/sh/v3/syntax, the parser behind
// shfmt), so a `;` inside a sed script, a `>` inside $(( )), and a `cd` that is
// an argument rather than a command are each what they are. Matching bytes had
// to guess at all three.

const (
	semicolonMessage = "must not separate commands with `;` -- put each command on its own line (cmd takes a `|` block scalar), and let errexit stop the line that fails"

	andListMessage = "must not chain commands with `&&` -- put each command on its own line; the line that fails ends the command, so && only restates it. Write `|| true` where a command may fail on purpose"

	redirectMessage = "must not redirect -- dats captures stdout and stderr, and a redirect takes away what the assertions read. Feed input with inputs.stdin or inputs.files/inputs.copy, write a file with the command's own output flag (or tee), and assert on outputs.stdout, outputs.stderr and outputs.files"

	heredocMessage = "must not use a shell heredoc (<<WORD) -- write the file and pull it in with inputs.files/inputs.copy or shared.files/shared.copy instead"

	herestringMessage = "must not use a shell herestring (<<<) -- use inputs.stdin instead of redirecting from the end of the line"

	cdMessage = "must not cd -- set workdir, which every command in the file (or the test) then runs in, so the command reads the same wherever the run started"
)

// checkShellCommand reports why a command may not run as a dats command, empty
// when it may. Text no shell can parse is reported as the parse error it is.
func checkShellCommand(s string) string {
	file, err := syntax.NewParser().Parse(strings.NewReader(s), "")
	if err != nil {
		return "is not valid shell: " + err.Error()
	}

	var found string
	syntax.Walk(file, func(node syntax.Node) bool {
		if found != "" {
			return false
		}
		switch n := node.(type) {
		case *syntax.Stmt:
			// A statement written `a; b` carries the semicolon; a newline-
			// separated one carries none.
			if n.Semicolon.IsValid() {
				found = semicolonMessage
			}
		case *syntax.BinaryCmd:
			// `||` stays legal: it is how a command says a failure is expected,
			// and under errexit there is no other way to say it.
			if n.Op == syntax.AndStmt {
				found = andListMessage
			}
		case *syntax.Redirect:
			found = redirectFinding(n.Op)
		case *syntax.CallExpr:
			if isCdCall(n) {
				found = cdMessage
			}
		}
		return found == ""
	})
	return found
}

// redirectFinding names the redirection, so the reader is pointed at the key
// that replaces the operator they wrote.
func redirectFinding(op syntax.RedirOperator) string {
	switch op {
	case syntax.Hdoc, syntax.DashHdoc:
		return heredocMessage
	case syntax.WordHdoc:
		return herestringMessage
	}
	return redirectMessage
}

// isCdCall reports whether the call runs cd, rather than merely naming it.
func isCdCall(call *syntax.CallExpr) bool {
	return len(call.Args) > 0 && call.Args[0].Lit() == "cd"
}
