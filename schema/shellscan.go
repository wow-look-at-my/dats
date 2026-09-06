package schema

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// A dats command is a command, not a shell script. Everything rejected here has
// a schema key that does the same job where the runner can see it.
//
// The check runs on a real parse (mvdan.cc/sh/v3/syntax, the parser behind
// shfmt), so a `;` inside a sed script, a `>` inside $(( )), and a `cd` used as
// an argument are each what they are. Matching bytes had to guess.

const (
	semicolonMessage = "must not separate commands with `;` -- put each command on its own line (cmd takes a `|` block scalar), and let errexit stop the line that fails"

	andListMessage = "must not chain commands with `&&` -- put each command on its own line; the line that fails ends the command, so && only restates it. Write `|| true` where a command may fail on purpose"

	redirectMessage = "must not redirect to a file -- dats captures stdout and stderr, and a redirect sends them where no assertion can read them. Feed input with inputs.stdin or inputs.files/inputs.copy, write a file with the command's own output flag (or tee), and assert on outputs.stdout, outputs.stderr and outputs.files. Moving output between the two captured streams (>&2, 2>&1) stays legal"

	heredocMessage = "must not use a shell heredoc (<<WORD) -- write the file and pull it in with inputs.files/inputs.copy or shared.files/shared.copy instead"

	herestringMessage = "must not use a shell herestring (<<<) -- use inputs.stdin instead of redirecting from the end of the line"

	cdMessage = "must not cd -- set workdir, which every command in the file (or the test) then runs in, so the command reads the same wherever the run started"
)

// checkShellCommand reports why a command may not run as a dats command, empty
// when it may. Text no shell can parse is reported as the parse error it is.
func checkShellCommand(s string) string {
	file, err := syntax.NewParser().Parse(strings.NewReader(s), "")
	if err != nil {
		// An unterminated heredoc fails the parse before the walk sees it. The
		// author still wrote a heredoc, so name the key that replaces it.
		if strings.Contains(err.Error(), "here-document") {
			return heredocMessage
		}
		return "is not valid shell: " + err.Error()
	}

	var found string
	syntax.Walk(file, func(node syntax.Node) bool {
		if found != "" {
			return false
		}
		switch n := node.(type) {
		case *syntax.Stmt:
			// `a; b` carries the semicolon; newline-separated carries none.
			if n.Semicolon.IsValid() {
				found = semicolonMessage
			}
		case *syntax.BinaryCmd:
			// `||` stays legal: under errexit it is the only way to say a
			// failure is expected.
			if n.Op == syntax.AndStmt {
				found = andListMessage
			}
		case *syntax.Redirect:
			found = redirectFinding(n)
		case *syntax.CallExpr:
			if isCdCall(n) {
				found = cdMessage
			}
		}
		return found == ""
	})
	return found
}

// redirectFinding names the key that replaces the operator the author wrote,
// and reports empty for a redirection that keeps the bytes inside dats.
func redirectFinding(r *syntax.Redirect) string {
	switch r.Op {
	case syntax.Hdoc, syntax.DashHdoc:
		return heredocMessage
	case syntax.WordHdoc:
		return herestringMessage
	case syntax.DplOut, syntax.DplIn:
		// Moving output between the streams dats captures lets every assertion
		// still read it. Any other descriptor escapes, like a file would.
		if target := r.Word.Lit(); target == "1" || target == "2" {
			return ""
		}
	}
	return redirectMessage
}

// isCdCall reports whether the call runs cd, rather than merely naming it.
func isCdCall(call *syntax.CallExpr) bool {
	return len(call.Args) > 0 && call.Args[0].Lit() == "cd"
}
