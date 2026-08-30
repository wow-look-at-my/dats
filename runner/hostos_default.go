//go:build !cosmo

package runner

import "runtime"

// detectHostOS answers the OS this binary was built for, which is the host it
// runs on for every target except cosmo.
func detectHostOS() string {
	return runtime.GOOS
}
