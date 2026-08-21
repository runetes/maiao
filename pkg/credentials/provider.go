package credentials

import (
	"github.com/99designs/keyring"
)

type providerEnvConfig struct {
	passwordKey string
	usernameKey string
}

var envVarForProvider = map[string]providerEnvConfig{
	"github":    {passwordKey: "GITHUB_TOKEN"},
	"gitlab":    {passwordKey: "GITLAB_TOKEN"},
	"gitea":     {passwordKey: "GITEA_TOKEN"},
	"forgejo":   {passwordKey: "FORGEJO_TOKEN"},
	"bitbucket": {passwordKey: "BITBUCKET_TOKEN", usernameKey: "BITBUCKET_USERNAME"},
}

func CredentialGetterForProvider(providerType string) CredentialGetter {
	cfg := envVarForProvider[providerType]
	if cfg.passwordKey == "" {
		cfg = envVarForProvider["github"]
	}

	getters := []CredentialGetter{
		&EnvToken{PasswordKey: cfg.passwordKey, UsernameKey: cfg.usernameKey},
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
