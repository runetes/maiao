package credentials

import (
	"github.com/99designs/keyring"
)

var envVarForProvider = map[string]string{
	"github":    "GITHUB_TOKEN",
	"gitlab":    "GITLAB_TOKEN",
	"gitea":     "GITEA_TOKEN",
	"forgejo":   "FORGEJO_TOKEN",
	"bitbucket": "BITBUCKET_TOKEN",
}

func CredentialGetterForProvider(providerType string) CredentialGetter {
	envKey := envVarForProvider[providerType]
	if envKey == "" {
		envKey = "GITHUB_TOKEN"
	}

	getters := []CredentialGetter{
		&EnvToken{PasswordKey: envKey},
		&Netrc{},
		&GitCredentials{GitPath: "git"},
	}

	kr, err := NewKeyring(keyring.Config{
		ServiceName: "maiao",
	})
	if err == nil {
		getters = append(getters, kr)
	}

	return ChainCredentialGetter(getters)
}
