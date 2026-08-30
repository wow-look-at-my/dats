package runner

import "strings"

// hostGOOS names the host dats runs on; a test points it at another host.
var hostGOOS = detectHostOS()

// hostCommandPath spells a path the way bash reads it: bash eats an NT
// backslash as an escape, so `C:\x` would reach the command as `C:x`.
func hostCommandPath(local string) string {
	if hostGOOS != "windows" {
		return local
	}
	return strings.ReplaceAll(local, `\`, "/")
}
