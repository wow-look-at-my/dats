package schema

import "strings"

// A dats command is one command, not a shell script. Everything this file
// rejects has a schema key that does the same job where the runner can see it:
// a redirect hides from dats exactly the output dats exists to assert on, a
// separator turns one test into several whose failures the exit code hides,
// and a cd makes the command's meaning depend on where the reader started.

// shellFinding is one rejected construct: the offset it starts at and why.
type shellFinding struct {
	Offset  int
	Message string
}

const (
	semicolonMessage = "must not chain commands with `;` -- put each command on its own line (cmd takes a block scalar), and let the shell's errexit stop the line that fails"

	andListMessage = "must not chain commands with `&&` -- put each command on its own line; the line that fails ends the command, so && only restates it. Use `|| true` to let one command fail on purpose"

	redirectMessage = "must not redirect -- dats captures stdout and stderr, and a redirect takes away what the assertions read. Feed input with inputs.stdin or inputs.files/inputs.copy, and assert on outputs.stdout, outputs.stderr or outputs.files"

	heredocMessage = "must not use a shell heredoc (<<WORD) -- write the file and pull it in with inputs.files/inputs.copy or shared.files/shared.copy instead"

	herestringMessage = "must not use a shell herestring (<<<) -- use inputs.stdin instead of redirecting from the end of the line"

	cdMessage = "must not cd -- set workdir, which every command in the file (or the test) then runs in, so the command reads the same wherever the run started"
)

// scanShell reports the first construct a dats command may not contain.
//
// The scan honors shell quoting and backslash escapes, so a `;` inside a sed
// script or a `>` inside an awk program is data and stays legal. It is not a
// shell parser: it recognizes the operators at the top level of the text, which
// is where a command turns into a script.
func scanShell(s string) *shellFinding {
	atCommandStart := true
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			i++ // The escaped byte is data, whatever it is.
		case '\'':
			if end := strings.IndexByte(s[i+1:], '\''); end >= 0 {
				i += end + 1
			} else {
				i = len(s)
			}
		case '"':
			i = skipDoubleQuoted(s, i)
		case ';':
			return &shellFinding{Offset: i, Message: semicolonMessage}
		case '&':
			if i+1 < len(s) && s[i+1] == '&' {
				return &shellFinding{Offset: i, Message: andListMessage}
			}
			if i+1 < len(s) && s[i+1] == '>' {
				return &shellFinding{Offset: i, Message: redirectMessage}
			}
			atCommandStart = true
		case '<':
			if strings.HasPrefix(s[i:], "<<<") {
				return &shellFinding{Offset: i, Message: herestringMessage}
			}
			if strings.HasPrefix(s[i:], "<<") {
				return &shellFinding{Offset: i, Message: heredocMessage}
			}
			return &shellFinding{Offset: i, Message: redirectMessage}
		case '>':
			return &shellFinding{Offset: i, Message: redirectMessage}
		case '\n', '|', '(', '{':
			atCommandStart = true
		case ' ', '\t':
			// Whitespace neither starts nor ends a command word.
		default:
			if atCommandStart && isCdWord(s[i:]) {
				return &shellFinding{Offset: i, Message: cdMessage}
			}
			atCommandStart = false
		}
	}
	return nil
}

// skipDoubleQuoted returns the index of the closing quote of the run starting at
// the quote s[open], or the end of s when the run never closes. A backslash
// inside double quotes still escapes, so an escaped quote does not close it.
func skipDoubleQuoted(s string, open int) int {
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return len(s)
}

// isCdWord reports whether s opens with cd as a whole command word.
func isCdWord(s string) bool {
	rest, found := strings.CutPrefix(s, "cd")
	if !found {
		return false
	}
	if rest == "" {
		return true
	}
	switch rest[0] {
	case ' ', '\t', '\n', ';', '&', '|':
		return true
	}
	return false
}

// checkShellCommand reports why a command text may not run as a dats command.
func checkShellCommand(s string) string {
	if finding := scanShell(s); finding != nil {
		return finding.Message
	}
	return ""
}
