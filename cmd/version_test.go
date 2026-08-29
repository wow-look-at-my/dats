package cmd

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionFromBuildInfo(t *testing.T) {
	buildInfo := func(mainVersion string, settings map[string]string) *debug.BuildInfo {
		info := &debug.BuildInfo{}
		info.Main.Version = mainVersion
		for key, value := range settings {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
		}
		return info
	}

	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "release version",
			info: buildInfo("v1.2.3", nil),
			want: "v1.2.3",
		},
		{
			name: "devel with clean vcs revision",
			info: buildInfo("(devel)", map[string]string{
				"vcs.revision": "0123456789abcdef0123456789abcdef01234567",
				"vcs.modified": "false",
			}),
			want: "0123456789ab",
		},
		{
			name: "devel with dirty vcs revision",
			info: buildInfo("(devel)", map[string]string{
				"vcs.revision": "0123456789abcdef0123456789abcdef01234567",
				"vcs.modified": "true",
			}),
			want: "0123456789ab+dirty",
		},
		{
			name: "short revision is not truncated",
			info: buildInfo("(devel)", map[string]string{"vcs.revision": "abc123"}),
			want: "abc123",
		},
		{
			name: "devel without vcs info",
			info: buildInfo("(devel)", nil),
			want: "unknown",
		},
		{
			name: "empty version without vcs info",
			info: buildInfo("", nil),
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, versionFromBuildInfo(tt.info))
		})
	}
}

func TestVersionStringNonEmpty(t *testing.T) {
	assert.NotEmpty(t, versionString())
	assert.Equal(t, versionString(), rootCmd.Version)
}
