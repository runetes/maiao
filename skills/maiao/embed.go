// Package skill carries the skill that git review install --skill writes out.
//
// The files are embedded from the directory they are also published from, rather
// than copied under pkg/, so that a binary can only ever install the skill that
// shipped with it. A vendored second copy would drift, and a skill describing a
// maiao you are not running is worse than having none: it is read as
// authoritative.
//
// A Go file inside the published plugin directory is ignored by the assistants
// that read it, which look only for .claude-plugin and skills.
package skill

import "embed"

// GitReviewRoot is the directory inside Files holding the skill, and the layout
// an assistant expects: SKILL.md beside the companion files its links name.
const GitReviewRoot = "skills/git-review"

// GitReview is the git review skill: SKILL.md and every file it links to.
//
// The whole directory is embedded rather than SKILL.md alone, because SKILL.md
// carries only the model and the four operations and points at the companions for
// everything else. Installing the entry point without them leaves every link
// dangling, which is worse than the single oversized file it replaced: the reader
// cannot tell the detail is missing rather than absent.
//
//go:embed skills/git-review
var GitReview embed.FS
