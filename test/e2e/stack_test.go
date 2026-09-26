//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// maiaoBinary is built once for the whole run by TestMain.
//
// The tests drive the binary rather than calling Review in process, because the rebase
// a fixup triggers creates the pull requests from a maiao that `git rebase` execs as
// the last step of its todo list. In process, that path does not exist.
var maiaoBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "maiao-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	maiaoBinary = filepath.Join(dir, "maiao")
	build := exec.Command("go", "build", "-o", maiaoBinary, "./cmd/maiao")
	build.Dir = ".." + string(filepath.Separator) + ".."
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building maiao: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// change is one commit in the stack under test, and the branch and pull request maiao
// gives it.
type change struct {
	name     string
	changeID string
	branch   string
	sha      string
}

// stack is a throwaway clone with a stack of changes on it, and the forge it reviews
// to.
type stack struct {
	t     *testing.T
	forge forge
	dir   string
	// home is a HOME of this test's own, holding the .netrc maiao authenticates from,
	// so a run depends on nothing about the machine and leaves nothing on it.
	home          string
	defaultBranch string
	changes       []change
}

// redact removes every credential from text before it is logged.
//
// Needed because a failing command is logged with its arguments, and the clone URL
// carries the credential — which went straight into the test output the first time this
// ran. Test logs end up in CI, so a secret printed once is a secret leaked.
func (s *stack) redact(text string) string {
	for _, secret := range s.forge.Secrets() {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "<redacted>")
		// A URL-encoded credential does not match the raw one: url.UserPassword escapes
		// it, and that is the form the clone URL carries.
		if escaped := url.QueryEscape(secret); escaped != secret {
			text = strings.ReplaceAll(text, escaped, "<redacted>")
		}
	}
	return text
}

// logf logs through the redactor, which is the only way this package should log.
func (s *stack) logf(format string, args ...any) {
	s.t.Helper()
	s.t.Log(s.redact(fmt.Sprintf(format, args...)))
}

// fatalf fails through the redactor.
func (s *stack) fatalf(format string, args ...any) {
	s.t.Helper()
	s.t.Fatal(s.redact(fmt.Sprintf(format, args...)))
}

// reviewResult is maiao's own --json output, which is how a test learns what it
// thought it did.
type reviewResult struct {
	Changes []struct {
		ChangeID string `json:"change_id"`
		Branch   string `json:"branch"`
		URL      string `json:"url"`
		ID       string `json:"id"`
		Status   string `json:"status"`
	} `json:"changes"`
	StackID string `json:"stack_id"`
	Error   *struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	} `json:"error"`
}

// newStack clones the throwaway repository and puts n changes on it, each with its own
// Change-Id.
//
// The Change-Ids are random per stack, so two runs of these tests — or two of them at
// once — never contend for the same branch or pull request.
func newStack(t *testing.T, f forge, n int) *stack {
	t.Helper()
	dir := t.TempDir()
	s := &stack{
		t: t, forge: f,
		dir:  filepath.Join(dir, "repo"),
		home: filepath.Join(dir, "home"),
	}
	if err := os.MkdirAll(s.home, 0o700); err != nil {
		t.Fatal(err)
	}
	// 0600, as netrc requires, and inside a HOME this test owns: maiao's credential
	// lookup is per host and reads ~/.netrc, so this is how it authenticates without
	// anything being true of the machine.
	if err := os.WriteFile(filepath.Join(s.home, ".netrc"),
		[]byte(strings.Join(f.Netrc(), "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s.run(dir, "git", "clone", "--quiet", f.CloneURL(), s.dir)
	// The remote must not carry the credential: maiao derives the API host from it, and
	// the .netrc above is what authenticates the push.
	s.run(s.dir, "git", "remote", "set-url", "origin", f.RemoteURL())
	s.run(s.dir, "git", "config", "user.name", "maiao end-to-end test")
	s.run(s.dir, "git", "config", "user.email", "maiao-e2e@example.invalid")
	// Nothing here may reach the OS keyring: there is no terminal for it to prompt on,
	// and the credential it would ask for is in the .netrc already.
	s.run(s.dir, "git", "config", "maiao.keyring", "disabled")
	for _, cfg := range f.GitConfig() {
		s.run(s.dir, "git", "config", cfg[0], cfg[1])
	}
	// maiao refuses to review a repository whose commit message hook is missing, even
	// when every commit already carries a Change-Id.
	s.run(s.dir, maiaoBinary, "install")

	s.defaultBranch = strings.TrimSpace(s.run(s.dir, "git", "symbolic-ref", "--short", "HEAD"))

	for i := 0; i < n; i++ {
		name := string(rune('A' + i))
		id := "I" + randomHex(20)
		path := filepath.Join(s.dir, fmt.Sprintf("file-%s-%s.txt", name, id[1:9]))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("%s %d\n", name, time.Now().UnixNano())), 0o644); err != nil {
			t.Fatal(err)
		}
		s.run(s.dir, "git", "add", "-A")
		s.run(s.dir, "git", "commit", "--quiet", "-m",
			fmt.Sprintf("test: %s\n\nChange-Id: %s", name, id))
		s.changes = append(s.changes, change{
			name:     name,
			changeID: id,
			branch:   "maiao." + id,
			sha:      strings.TrimSpace(s.run(s.dir, "git", "rev-parse", "HEAD")),
		})
	}

	t.Cleanup(s.cleanup)
	return s
}

// review runs `git review` and returns what it reported.
//
// A failure is returned rather than fatal: several tests are about what a review does
// when something has gone wrong, and the log is attached either way because it is the
// only account of what the review decided.
func (s *stack) review(label string) (reviewResult, error) {
	s.t.Helper()
	cmd := exec.Command(maiaoBinary, "-v", "4", "--batch", "--json")
	cmd.Dir = s.dir
	cmd.Env = s.env()
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()

	var result reviewResult
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal([]byte(stdout.String()), &result); jsonErr != nil {
			s.logf("[%s] stdout was not the documented JSON: %v\n%s", label, jsonErr, stdout.String())
		}
	}
	s.logf("[%s] git review exited %v; result %+v", label, err, result)
	if err != nil {
		s.logf("[%s] log tail:\n%s", label, tail(stderr.String(), 25))
	}
	return result, err
}

// env is the environment every command in the fixture runs with.
//
// HOME is the test's own, so the .netrc maiao authenticates from is this one and not
// the developer's, and the terminal prompt is off so a missing credential fails
// instead of hanging on a password prompt no one can answer.
func (s *stack) env() []string {
	return append(os.Environ(), "HOME="+s.home, "GIT_TERMINAL_PROMPT=0")
}

// run executes a command in dir and returns its output, failing the test — with every
// credential redacted — if it does not succeed.
func (s *stack) run(dir string, name string, args ...string) string {
	s.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = s.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// reorder rebuilds the stack in the given order, by index into the original changes.
//
// Cherry-picked rather than rebased interactively: the commit messages carry the
// Change-Ids, so the changes keep their identity and their pull requests while their
// commits are rebuilt, which is exactly what a reorder does.
func (s *stack) reorder(order ...int) {
	s.t.Helper()
	run(s.t, s.dir, nil, "git", "reset", "--hard", "--quiet", "origin/"+s.defaultBranch)
	for _, i := range order {
		run(s.t, s.dir, nil, "git", "cherry-pick", "--quiet", s.changes[i].sha)
	}
	s.t.Logf("stack is now:\n%s", run(s.t, s.dir, nil, "git", "log", "--oneline",
		"origin/"+s.defaultBranch+"..HEAD"))
}

// fixup amends the change at index i the way a review comment is answered, by adding a
// `fixup!` commit on top rather than rewriting history.
//
// It is not adjacent to its target, so the next review has to rebase: maiao's todo
// picks the fixup directly after the commit it fixes, squashing it in and rebuilding
// every change above it.
func (s *stack) fixup(i int) {
	s.t.Helper()
	c := s.changes[i]
	path := filepath.Join(s.dir, fmt.Sprintf("file-%s-%s.txt", c.name, c.changeID[1:9]))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(f, "fixed up %d\n", time.Now().UnixNano()); err != nil {
		f.Close()
		s.t.Fatal(err)
	}
	f.Close()
	run(s.t, s.dir, nil, "git", "add", "-A")
	run(s.t, s.dir, nil, "git", "commit", "--quiet", "--fixup", c.sha)
}

// only is the single pull request a change is expected to have, and fails the test
// when there is not exactly one — a second one for the same change is the duplicate
// this whole exercise is about.
//
// One request for the one change asked about, not the whole stack: the assertions call
// this per change and then again per base, and listing every head each time turned a
// four-change test into dozens of calls against a live forge.
func (s *stack) only(changeIndex int) pullRequest {
	s.t.Helper()
	c := s.changes[changeIndex]
	prs, err := s.forge.PullRequestsForHead(context.Background(), c.branch)
	if err != nil {
		s.fatalf("listing pull requests for %s: %v", c.branch, err)
	}
	if len(prs) != 1 {
		s.fatalf("change %s has %d pull requests, want exactly 1: %v", c.name, len(prs), prs)
	}
	return prs[0]
}

// cleanup closes whatever the test opened and deletes its branches, so a throwaway
// repository does not fill up with the leavings of every run.
//
// Best effort: a failure here is logged, never failed on, because it would mask the
// result of the test itself.
func (s *stack) cleanup() {
	if os.Getenv("MAIAO_E2E_KEEP") == "1" {
		s.t.Logf("MAIAO_E2E_KEEP=1, leaving the pull requests and branches behind")
		return
	}
	ctx := context.Background()
	for _, c := range s.changes {
		prs, err := s.forge.PullRequestsForHead(ctx, c.branch)
		if err != nil {
			s.t.Logf("cleanup: listing %s: %v", c.branch, err)
			continue
		}
		for _, pr := range prs {
			if pr.State == "open" {
				if err := s.forge.Close(ctx, pr.Number); err != nil {
					s.t.Logf("cleanup: closing #%s: %v", pr.Number, err)
				}
			}
		}
		// Only ever a branch this stack created: the name carries its own random
		// Change-Id, so it can belong to nothing else.
		if err := s.forge.DeleteBranch(ctx, c.branch); err != nil {
			s.t.Logf("cleanup: deleting %s: %v", c.branch, err)
		}
	}
}

// run executes a command in dir and returns its stdout, failing the test if it does
// not succeed. extraEnv is added to the inherited environment.
func run(t *testing.T, dir string, extraEnv []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if extraEnv != nil {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func tail(s string, lines int) string {
	split := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(split) > lines {
		split = split[len(split)-lines:]
	}
	return strings.Join(split, "\n")
}
