#!/bin/sh
#
# Downloads the latest Gerrit commit-msg hook from the upstream repository and
# writes it to pkg/gerrit/commit-msg.sh.
#
# Usage:
#   ./scripts/update-gerrit-hook.sh              # latest on master
#   ./scripts/update-gerrit-hook.sh <commit-sha>  # specific commit

set -eu

REPO="GerritCodeReview/gerrit"
HOOK_PATH="resources/com/google/gerrit/server/tools/root/hooks/commit-msg"
DEST="$(cd "$(dirname "$0")/.." && pwd)/pkg/gerrit/commit-msg.sh"

ref="${1:-master}"

echo "Fetching commit-msg hook from ${REPO}@${ref}..."
content="$(gh api "repos/${REPO}/contents/${HOOK_PATH}" -f ref="${ref}" --jq '.content' | base64 -d)"

if [ -z "${content}" ]; then
  echo "Error: failed to fetch the hook content." >&2
  exit 1
fi

printf '%s\n' "${content}" > "${DEST}"
chmod +x "${DEST}"

echo "Updated ${DEST}"
