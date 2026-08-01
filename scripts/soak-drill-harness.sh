#!/usr/bin/env bash
# Soak drill harness — PLANS.md Phase 8, D-P8-2.
#
# Emits a Claude Code limit banner on demand, accepts exactly one resume, then
# erases the banner so resume verification observes cleared evidence.
#
# Deliberately not `cat`: cat echoes the injected text but never removes the
# banner, so verification would still see limit evidence and the drill would
# fail for a reason that has nothing to do with the transport under test.
#
# The pane runs no agent, so the provider registry resolves by content and
# requires exactly one match. The banner below matches Claude's detector and
# none of Codex's identity patterns — verified with `herdr-auto-resume detect`
# against both providers before this harness was used as evidence.
#
# Usage: soak-drill-harness.sh [trigger-file]
#   Idles until the trigger file appears, fires once, then stays alive so the
#   pane keeps its identity for the remainder of the soak.

set -uo pipefail

TRIGGER="${1:-${XDG_RUNTIME_DIR:-/tmp}/herdr-auto-resume-drill.trigger}"

# Far enough out that the watcher persists a WAITING job rather than resuming on
# the spot; close enough that the drill completes in minutes.
RESET_OFFSET_MIN="${RESET_OFFSET_MIN:-2}"

# ESC[3J clears the scrollback too. Plain `clear` only scrolls the banner out of
# the viewport, and a pane read that includes scrollback would still find it.
wipe() { printf '\033[H\033[2J\033[3J'; }

# The idle screen must ALREADY identify as Claude. A watcher enables a pane only
# at startup and only if its provider resolved then (runcmd.go EnableAll), so a
# harness that idles as a plain shell is monitored forever but never actionable —
# the T+48h drill would silently do nothing. Box drawing plus a line starting
# "> " satisfies detection.IsClaudeCode without tripping any limit signal.
chrome() {
	cat <<'EOF'
╭──────────────────────────────────────────────────────────────╮
│ soak drill harness                                           │
╰──────────────────────────────────────────────────────────────╯
>
EOF
}

rm -f "$TRIGGER"
wipe
chrome
printf '\nidle — waiting for %s\n' "$TRIGGER"

while [ ! -e "$TRIGGER" ]; do
	sleep 2
done
rm -f "$TRIGGER"

reset_at=$(date -d "+${RESET_OFFSET_MIN} minutes" '+%-I:%M%P')
wipe
chrome
printf '  ⎿  limit reached ∙ resets %s\n\n' "$reset_at"

# An idle Claude-identified pane receives a periodic nudge every 15 minutes, so
# stray "continue" text may already be sitting in the input buffer. Discard it:
# the blocking read below must consume the REAL resume, not a buffered nudge,
# or the drill reports success without a resume having happened. Safe because
# the real resume cannot arrive until reset time plus margin, minutes from now.
while read -r -t 0.05 _; do :; done

# One line of input is the injected resume. Claude's resume action sends Esc
# before the text, so the line arrives with a leading control byte; any line
# counts, and the content is deliberately not inspected.
#
# EOF is NOT a resume. An unguarded read would fall through on a closed stdin,
# wipe the banner, and print the success marker — fabricating the exact evidence
# this drill exists to produce. On EOF the banner is left untouched and nothing
# is printed, because added output would also satisfy the watcher's
# changed-output verification path.
if IFS= read -r _; then
	wipe
	printf 'DRILL-RESUMED-OK %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
fi

while :; do
	sleep 3600
done
