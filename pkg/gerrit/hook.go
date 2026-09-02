package gerrit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adevinta/maiao/pkg/system"
	"github.com/spf13/afero"
)

// ChainedHookName is the name maiao installs its hook under when something else
// already owns the commit message hook.
//
// It doubles as the marker that recognises a chained hook: the call ChainCall
// writes mentions it, so a hook that delegates to maiao's is not mistaken for one
// that ignores it.
const ChainedHookName = "maiao-commit-msg"

// changeIDMarker recognises a hook that gives commits a Change-Id.
//
// The hook is identified by what it is for rather than by being byte identical to
// the one this version downloads, so a hook installed by an older maiao, or a
// different revision of the same gerrit script, still counts as installed.
const changeIDMarker = "Change-Id"

// shells are the interpreters ChainCall's syntax is valid in.
//
// A hook written in anything else is left for its owner to extend by hand:
// inserting POSIX shell into, say, a python hook would break the hook outright,
// which is worse than not chaining at all. fish is deliberately absent — its name
// ends in sh but it shares none of the syntax.
var shells = map[string]bool{
	"sh": true, "bash": true, "dash": true, "ash": true, "ksh": true, "mksh": true, "zsh": true,
}

// HookState describes what sits at a hook path.
type HookState int

const (
	// NoHook means there is nothing there.
	NoHook HookState = iota
	// MaiaoHook means a hook commits will get a Change-Id from, whether it is the
	// one maiao installs or another hook chaining to it.
	MaiaoHook
	// ForeignHook means a hook maiao did not write. Overwriting it would take away
	// whatever it does, silently, so nothing may write over it without being told
	// to.
	ForeignHook
)

// String names the state, because it is logged, and the log is where someone
// debugging commits that have no Change-Id looks.
func (s HookState) String() string {
	switch s {
	case NoHook:
		return "absent"
	case MaiaoHook:
		return "maiao"
	case ForeignHook:
		return "installed by something else"
	}
	return fmt.Sprintf("unknown (%d)", int(s))
}

// StateAt reports what is installed at a hook path.
//
// The file is read rather than merely stated, because the question is not whether
// a hook exists but whether commits will come out with a Change-Id. Hook managers
// such as husky, lefthook and pre-commit put their own commit message hook
// exactly where maiao's belongs, and counting that as maiao's is what makes the
// failure silent: no Change-Id is ever added, and maiao never says why.
func StateAt(path string) HookState {
	content, err := afero.ReadFile(system.DefaultFileSystem, path)
	if err != nil {
		return NoHook
	}
	if bytes.Contains(content, []byte(changeIDMarker)) || bytes.Contains(content, []byte(ChainedHookName)) {
		return MaiaoHook
	}
	return ForeignHook
}

// ChainCall is the line that makes a hook run the hook of the given name sitting
// beside it.
//
// The path is derived from $0 rather than from the repository, because a hook is
// invoked with its own path and so already knows where it lives. That keeps the
// line correct in a worktree, under core.hooksPath, and inside a git template
// directory that will be copied into repositories that do not exist yet.
func ChainCall(name string) string {
	return fmt.Sprintf(`"$(dirname -- "$0")/%s" "$1" || exit 1`, name)
}

// Chainable reports whether ChainTo can extend the hook at path.
func Chainable(path string) bool {
	content, err := afero.ReadFile(system.DefaultFileSystem, path)
	if err != nil {
		return false
	}
	return isShellScript(content)
}

// ChainTo makes the hook at path also run the hook of the given name beside it,
// so that a hook maiao did not write keeps doing what it did and commits still
// get a Change-Id.
//
// The call goes directly after the shebang rather than at the end: a hook whose
// last line is `exit 0`, or that hands over with `exec`, would never reach
// anything appended. Running first also means the checks the hook already
// performs see the final message, Change-Id included.
func ChainTo(path, name string) error {
	content, err := afero.ReadFile(system.DefaultFileSystem, path)
	if err != nil {
		return err
	}
	if !isShellScript(content) {
		return fmt.Errorf("%s is not a shell script", path)
	}
	// Running twice must not add the call twice, since the hook is looked at again
	// on every review.
	if bytes.Contains(content, []byte(name)) {
		return nil
	}
	shebang, rest, _ := bytes.Cut(content, []byte("\n"))
	chained := bytes.Join([][]byte{
		shebang,
		[]byte("\n# Added by maiao, so that the commit gets a Change-Id.\n"),
		[]byte(ChainCall(name)),
		[]byte("\n"),
		rest,
	}, nil)
	// Rewritten in place rather than replaced, so that it keeps its mode: a hook
	// that stops being executable stops running, and git says nothing about it.
	file, err := system.DefaultFileSystem.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(chained)
	return err
}

// isShellScript reports whether a script's shebang names an interpreter that
// understands ChainCall.
func isShellScript(content []byte) bool {
	line, _, _ := bytes.Cut(content, []byte("\n"))
	if !bytes.HasPrefix(line, []byte("#!")) {
		return false
	}
	fields := strings.Fields(string(line[2:]))
	if len(fields) == 0 {
		return false
	}
	interpreter := filepath.Base(fields[0])
	// `#!/usr/bin/env bash` names the interpreter in the next word instead.
	if interpreter == "env" && len(fields) > 1 {
		interpreter = filepath.Base(fields[1])
	}
	return shells[interpreter]
}
