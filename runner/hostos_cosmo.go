//go:build cosmo

package runner

import "runtime"

// detectHostOS asks the runtime where the APE landed: an APE reports
// GOOS "cosmo" on every host, so the compiled name says nothing.
func detectHostOS() string {
	return runtime.CosmoHostOS()
}
