package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTypeValid(t *testing.T) {
	tests := []struct {
		input    string
		expected Type
	}{
		{"github", GitHub},
		{"gitlab", GitLab},
		{"gitea", Gitea},
		{"forgejo", Forgejo},
		{"bitbucket", Bitbucket},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := ParseType(tt.input)
			assert.True(t, ok)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseTypeInvalid(t *testing.T) {
	tests := []string{"", "unknown", "GitHub", "GITLAB", "gerrit"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, ok := ParseType(input)
			assert.False(t, ok)
		})
	}
}

func TestTypeString(t *testing.T) {
	assert.Equal(t, "github", GitHub.String())
	assert.Equal(t, "gitlab", GitLab.String())
	assert.Equal(t, "gitea", Gitea.String())
	assert.Equal(t, "forgejo", Forgejo.String())
	assert.Equal(t, "bitbucket", Bitbucket.String())
}

func TestKnownHostsContainsExpectedEntries(t *testing.T) {
	assert.Equal(t, GitHub, KnownHosts["github.com"])
	assert.Equal(t, GitLab, KnownHosts["gitlab.com"])
	assert.Equal(t, Forgejo, KnownHosts["codeberg.org"])
	assert.Equal(t, Bitbucket, KnownHosts["bitbucket.org"])
}
