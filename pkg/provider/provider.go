package provider

type Type string

const (
	GitHub    Type = "github"
	GitLab    Type = "gitlab"
	Gitea     Type = "gitea"
	Forgejo   Type = "forgejo"
	Bitbucket Type = "bitbucket"
	Origin    Type = "origin"
)

var KnownHosts = map[string]Type{
	"github.com":        GitHub,
	"gitlab.com":        GitLab,
	"codeberg.org":      Forgejo,
	"bitbucket.org":     Bitbucket,
	"origin.cursor.com": Origin,
}

var AllTypes = []Type{GitHub, GitLab, Gitea, Forgejo, Bitbucket, Origin}

func (t Type) String() string {
	return string(t)
}

func ParseType(s string) (Type, bool) {
	switch Type(s) {
	case GitHub, GitLab, Gitea, Forgejo, Bitbucket, Origin:
		return Type(s), true
	default:
		return "", false
	}
}
