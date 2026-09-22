package credentials

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/runetes/maiao/pkg/log"
	sshclientconfig "github.com/tjamet/ssh-client-config"
	"golang.org/x/crypto/ssh"
)

type GitAuth struct {
	Endpoint    *transport.Endpoint
	Credentials CredentialGetter
}

var _ transport.AuthMethod = &GitAuth{}
var _ gitssh.AuthMethod = &GitAuth{}

func (a *GitAuth) SetAuth(r *http.Request) {
	creds, err := a.Credentials.CredentialForHost(r.Host)
	if err != nil || creds == nil {
		log.Logger.WithField("host", r.Host).WithError(err).Infof("failed to find credentials")
		return
	}
	r.SetBasicAuth(creds.Username, creds.Password)
}

// ClientConfig authenticates the way ssh(1) does, from ~/.ssh/config.
//
// go-git's DefaultAuthBuilder asks the agent and nothing else, so `git review`
// failed with "SSH agent requested but SSH_AUTH_SOCK not-specified" against
// hosts plain git reaches without trouble — anywhere the key is named by an
// IdentityFile rather than loaded into an agent. Reading the config covers both:
// the agent still supplies its keys unless IdentitiesOnly says otherwise.
func (a *GitAuth) ClientConfig() (*ssh.ClientConfig, error) {
	if a.Endpoint == nil {
		return nil, errors.New("no endpoint found")
	}

	overrides := []sshclientconfig.Override{}
	if a.Endpoint.User != "" {
		// The user in the remote URL is what git connects as, so it wins over a
		// User the config states for the host.
		overrides = append(overrides, sshclientconfig.WithUser(a.Endpoint.User))
	}
	cfg, err := sshclientconfig.NewSSHClientConfig("").SSHClientConfig(a.Endpoint.Host, overrides...)
	if err != nil {
		// The config reader refuses a file using an option it does not implement —
		// ForwardAgent and ProxyJump among them, and a `Host *` block setting one
		// applies to every host. Those users reached their remote through the agent
		// before this, so they keep doing so; taking the tool away from them to
		// gain IdentityFile support would be a poor trade. The reason is logged
		// rather than swallowed, because the identity the config names is not being
		// used.
		log.Logger.WithField("host", a.Endpoint.Host).WithError(err).
			Infof("falling back to the ssh agent: the ssh configuration could not be used")
		agent, agentErr := gitssh.DefaultAuthBuilder(a.Endpoint.User)
		if agentErr != nil {
			return nil, fmt.Errorf("failed to build the ssh configuration for %s: %w (the ssh agent was no use either: %v)", a.Endpoint.Host, err, agentErr)
		}
		return agent.ClientConfig()
	}

	// Not negotiable: that library builds its config with
	// ssh.InsecureIgnoreHostKey, and maiao's host key handling — the mismatch
	// error, the trust-on-first-use opt-in — is the knownhosts callback reporting
	// what it found.
	return (&gitssh.HostKeyCallbackHelper{}).SetHostKeyCallbackAndAlgorithms(cfg)
}

func (a *GitAuth) Name() string {
	return "auth from credentials"
}

func (a *GitAuth) String() string {
	return "auth from credentials"
}
