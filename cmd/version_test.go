package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionFrom(t *testing.T) {
	tests := []struct {
		name   string
		set    string // what a release build's -ldflags -X set
		module string // the main module version go records, "" with no build info
		want   string
	}{
		{"a release build", "0.3.0", "(devel)", "0.3.0"},
		{"go install of a tag", "dev", "v0.3.0", "0.3.0"},
		{"go build from a commit", "dev", "v0.2.3-0.20261029154835-7f03f6af0177", "0.2.3-0.20261029154835-7f03f6af0177"},
		{"go build with local changes", "dev", "v0.2.3-0.20261029154835-7f03f6af0177+dirty", "0.2.3-0.20261029154835-7f03f6af0177+dirty"},
		{"a build without VCS info", "dev", "(devel)", "dev"},
		{"no build info", "dev", "", "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, versionFrom(tt.set, tt.module))
		})
	}
}
