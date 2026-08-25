package provider

type Type string

const (
	GitHub    Type = "github"
	GitLab    Type = "gitlab"
	Gitea     Type = "gitea"
	Forgejo   Type = "forgejo"
	Bitbucket Type = "bitbucket"
)

var KnownHosts = map[string]Type{
	"github.com":    GitHub,
	"gitlab.com":    GitLab,
	"codeberg.org":  Forgejo,
	"bitbucket.org": Bitbucket,
}

var AllTypes = []Type{GitHub, GitLab, Gitea, Forgejo, Bitbucket}

func (t Type) String() string {
	return string(t)
}

func ParseType(s string) (Type, bool) {
	switch Type(s) {
	case GitHub, GitLab, Gitea, Forgejo, Bitbucket:
		return Type(s), true
	default:
		return "", false
	}
}
