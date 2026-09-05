package credentials

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
	"origin":    {passwordKey: "ORIGIN_TOKEN"},
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

	// Last in the chain: the password manager is the only getter that asks the
	// user for credentials, so it must run only once nothing else has answered.
	if kr, ok := keyringGetter(KeyringModeFromGitConfig()); ok {
		getters = append(getters, kr)
	}

	return ChainCredentialGetter(getters)
}
