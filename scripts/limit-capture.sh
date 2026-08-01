#!/usr/bin/env bash
# Ground-truth capture for limit events that the watchers miss.
#
# Two real sessions hit usage limits on 2026-08-01 and produced no job, no state
# file, and no log line — the fail-closed paths are silent, so there is nothing
# to diagnose after the fact. This snapshots monitored panes whenever their text
# looks limit-ish, giving the next occurrence a verbatim artifact.
#
# Read-only: it uses the same `herdr pane read` the watchers use and never sends
# input, so it cannot disturb the running soak.

set -uo pipefail

OUT="${OUT:-$HOME/.local/state/herdr-auto-resume/limit-captures}"
PANES="${PANES:-wA:p1 wR:p1 wS:p1}"
INTERVAL="${INTERVAL:-60}"
COOLDOWN="${COOLDOWN:-300}"
LINES="${LINES:-200}"

# Deliberately broader than the detector's own patterns: the point is to catch
# text the detector may be REJECTING, so matching only what it accepts would
# reproduce the blind spot being investigated.
SIGNAL='limit reached|usage limit|hit your limit|resets? (at )?[0-9]|try again in|rate.?limit|out of (usage|credits)|upgrade your plan|extra usage'

mkdir -p "$OUT"

while :; do
	for pane in $PANES; do
		text=$(herdr pane read "$pane" --source detection --lines "$LINES" 2>/dev/null) || continue
		[ -z "$text" ] && continue
		printf '%s' "$text" | grep -qiE "$SIGNAL" || continue

		# Two independent bounds. Content hashing alone is not enough: an ACTIVE
		# pane changes every poll, so a busy session discussing limits would
		# capture on every tick. The per-pane cooldown bounds that case; the hash
		# bounds the opposite one, a static limit screen sitting there for hours.
		now=$(date -u '+%s')
		last_var="last_${pane//[:.-]/_}"
		last=${!last_var:-0}
		[ $((now - last)) -lt "$COOLDOWN" ] && continue

		hash=$(printf '%s' "$text" | sha256sum | cut -c1-12)
		stamp=$(date -u '+%Y%m%dT%H%M%SZ')
		file="$OUT/${pane//:/-}-$hash.txt"
		[ -e "$file" ] && continue
		printf -v "$last_var" '%s' "$now"

		{
			echo "# pane=$pane captured=$stamp"
			echo "# herdr pane read $pane --source detection --lines $LINES"
			echo "---"
			printf '%s\n' "$text"
		} > "$file"
		echo "$stamp captured $pane -> $file"
	done
	sleep "$INTERVAL"
done
