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
TRANSIENT_OUT="${TRANSIENT_OUT:-$OUT/transient}"
PANES="${PANES:-wA:p1 wR:p1 wS:p1}"
INTERVAL="${INTERVAL:-60}"
COOLDOWN="${COOLDOWN:-300}"
LINES="${LINES:-200}"

# Deliberately broader than the detector's own patterns: the point is to catch
# text the detector may be REJECTING, so matching only what it accepts would
# reproduce the blind spot being investigated.
SIGNAL='limit reached|usage limit|hit your limit|resets? (at )?[0-9]|try again in|rate.?limit|out of (usage|credits)|upgrade your plan|extra usage'
# Deliberately broad and provisional, for the same ground-truth purpose as
# SIGNAL. These are the BACKLOG 16 hypotheses, not validated provider captures.
TRANSIENT_SIGNAL='429|too many requests|5[0-9][0-9]|5xx|overloaded|temporarily limiting requests|api error: connection|connection (reset|refused|closed|failed)|network error'

mkdir -p "$OUT" "$TRANSIENT_OUT"

capture() {
	local out=$1 kind=$2 signal=$3 pane=$4 text=$5
	printf '%s' "$text" | grep -qiE "$signal" || return 0

	# The per-pane cooldown and content hash remain independent for each class:
	# a busy pane cannot produce a file every poll, and a static screen cannot
	# produce duplicate files forever.
	local now last_var last hash stamp file
	now=$(date -u '+%s')
	last_var="last_${kind}_${pane//[:.-]/_}"
	last=${!last_var:-0}
	[ $((now - last)) -lt "$COOLDOWN" ] && return 0

	hash=$(printf '%s' "$text" | sha256sum | cut -c1-12)
	stamp=$(date -u '+%Y%m%dT%H%M%SZ')
	file="$out/${pane//:/-}-$hash.txt"
	[ -e "$file" ] && return 0
	printf -v "$last_var" '%s' "$now"

	{
		echo "# pane=$pane captured=$stamp"
		echo "# herdr pane read $pane --source detection --lines $LINES"
		echo "---"
		printf '%s\n' "$text"
	} > "$file"
	echo "$stamp captured $kind $pane -> $file"
}

while :; do
	for pane in $PANES; do
		text=$(herdr pane read "$pane" --source detection --lines "$LINES" 2>/dev/null) || continue
		[ -z "$text" ] && continue
		capture "$OUT" limit "$SIGNAL" "$pane" "$text"
		capture "$TRANSIENT_OUT" transient "$TRANSIENT_SIGNAL" "$pane" "$text"
	done
	sleep "$INTERVAL"
done
