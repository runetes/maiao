package git

import (
	"path/filepath"
	"testing"

	"github.com/adevinta/maiao/pkg/testutil"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGoGitCannotReadGlobalHooksPath records why HookPath runs git as a
// subprocess instead of reading the config through go-git, which is what the
// rest of maiao does.
//
// go-git exposes core.hooksPath only through Raw, since Config.Core has no
// field for it, and Raw sections are not merged across configuration scopes. A
// value set in the user's global config is therefore invisible.
//
// This test asserts a limitation rather than a behaviour, which is unusual on
// purpose: if it starts failing, go-git has gained the ability and HookPath can
// drop the subprocess.
func TestGoGitCannotReadGlobalHooksPath(t *testing.T) {
	home := testutil.IsolateHome(t)
	repo := testutil.InitRepo(t)
	hooksPath := filepath.Join(home, "global-hooks")
	testutil.Cmd(t, "git", "config", "--global", "core.hooksPath", hooksPath)

	// git itself sees it, so the setup is sound and the gap is go-git's.
	require.Equal(t, hooksPath, testutil.CmdOutput(t, "git", "-C", repo, "config", "--get", "core.hooksPath"))

	r, err := gogit.PlainOpenWithOptions(repo, &gogit.PlainOpenOptions{DetectDotGit: true})
	require.NoError(t, err)

	for _, scope := range []struct {
		name  string
		scope config.Scope
	}{
		{"local", config.LocalScope},
		{"global", config.GlobalScope},
		{"system", config.SystemScope},
	} {
		cfg, err := r.ConfigScoped(scope.scope)
		require.NoError(t, err)
		assert.Empty(t, cfg.Raw.Section("core").Option("hooksPath"),
			"go-git unexpectedly read the global core.hooksPath in %s scope; HookPath may be able to stop shelling out to git", scope.name)
	}
}
