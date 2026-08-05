#!/usr/bin/env bash
# Menu drill harness — the live gate for BACKLOG 18.
#
# soak-drill-harness.sh proves the BANNER path: banner visible, watcher injects
# a resume. This harness proves the MENU path, which is a different failure mode
# and the one that shipped broken in v0.3.0.
#
# What v0.3.1 fixed, and therefore what this must reproduce:
#   At resume time a pane showing Claude's limit menu carries NO Claude chrome —
#   the menu owns the whole viewport. A 200-line `--source detection` read of the
#   two real panes on 2026-08-04 returned ~41 lines with zero Claude markers, so
#   detection.IsClaudeCode was false, the identity gate fired first, and the job
#   parked "pane is not claude" before the menu-answering branch could run. A
#   harness that keeps chrome on screen alongside the menu does NOT test this —
#   it tests the case that already worked. The menu screen here is therefore
#   deliberately bare.
#
# WHAT THIS HARNESS CAN AND CANNOT PROVE — read before trusting a result.
# Established by two live runs on 2026-08-05. A shell-script pane has no
# `agent_session`, and `answerLimitMenu` (internal/jobs/validate.go) deliberately
# refuses to press Enter unless BOTH a pane agent-session id and a job episode id
# are present. That guard is correct — it is what stops the watcher answering a
# menu it cannot tie to a known episode — and no fake pane can satisfy it.
# Therefore:
#   CAN prove: detection of the banner, job scheduling, and that a menu-only
#              viewport no longer parks at the identity gates (BACKLOG 18 + 20).
#              Reaching "menu answer refused: missing session or episode identity"
#              IS the pass signal for those two items — it means every identity
#              gate was cleared and only the session guard remains.
#   CANNOT prove: the menu keypress itself. That needs a REAL Claude pane showing
#              a REAL limit menu, i.e. the production event of 2026-08-04.
# Do not "fix" this by weakening the session guard to make the drill go green.
#
# SUCCESS IS NOT "RESUMED". Answering the menu selects "Stop and wait for limit
# to reset", after which Claude itself waits — so the job is expected to end
# MANUAL_REQUIRED with MenuAttempt recorded. A drill that demands RESUMED here
# would fail a working fix. The pass condition is:
#   1. this harness prints MENU-ANSWERED-OK  (the Enter keystroke arrived), and
#   2. the job records a MenuAttempt and does NOT say "pane is not claude".
#
# Usage: menu-drill-harness.sh [trigger-file]
#   Phase 1 idles as an identifiable Claude pane so the watcher enables it.
#   Phase 2 (on trigger) shows banner + chrome so detection creates a job.
#   Phase 3 (MENU_DELAY_SEC later) replaces the screen with the bare menu, which
#           is what the watcher must be looking at when the resume moment comes.

set -uo pipefail

TRIGGER="${1:-${XDG_RUNTIME_DIR:-/tmp}/herdr-auto-resume-menu-drill.trigger}"

# Far enough out that the watcher persists a WAITING job rather than acting on
# the spot, close enough to finish in minutes.
RESET_OFFSET_MIN="${RESET_OFFSET_MIN:-2}"

# The banner must be on screen long enough to be detected, then must be GONE
# before the resume moment — otherwise the pane still looks like the banner case
# and the menu path is never exercised.
MENU_DELAY_SEC="${MENU_DELAY_SEC:-25}"

# ESC[3J clears scrollback too; plain `clear` only scrolls content out of the
# viewport, where a detection read would still find it.
wipe() { printf '\033[H\033[2J\033[3J'; }

# Idle/banner screen: identifiable as Claude Code so the pane is enabled and the
# banner is attributed to the right provider.
chrome() {
	cat <<'EOF'
╭──────────────────────────────────────────────────────────────╮
│ menu drill harness                                           │
╰──────────────────────────────────────────────────────────────╯
>
EOF
}

# The bare menu — no chrome, no box, no "> " prompt line. Copied in shape from
# the real capture on w1F:p1 / w1B:p1. The ❯ must sit on option 1: the watcher
# matches that option by TEXT and refuses to answer if the highlighted line is
# anything else, so moving it here is how this drill would prove the safety gate
# still holds.
menu() {
	cat <<'EOF'
   What do you want to do?

   ❯ 1. Stop and wait for limit to reset
     2. Upgrade your plan
     3. Upgrade to Team plan

   Enter to confirm · Esc to cancel
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

sleep "$MENU_DELAY_SEC"

# Now the pane looks exactly like the real failure: menu only, no chrome.
wipe
menu

# An idle Claude-identified pane may have received a periodic nudge, leaving
# stray input buffered. Discard it — the read below must consume the REAL menu
# answer, not a leftover, or this drill reports success without the watcher
# having done anything.
while read -r -t 0.05 _; do :; done

# One keystroke is the menu answer. EOF is NOT an answer: an unguarded read
# would fall through on closed stdin and print the success marker, fabricating
# the evidence this drill exists to produce. On EOF, print nothing and leave the
# menu on screen.
if IFS= read -r _; then
	wipe
	printf 'MENU-ANSWERED-OK %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
fi

while :; do
	sleep 3600
done
