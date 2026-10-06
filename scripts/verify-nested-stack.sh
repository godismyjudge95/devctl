#!/bin/sh
# Fail unless every listed child is a descendant of the devctl daemon
# and the tree contains no brew or per-service systemctl. Reviewer reruns
# this against a live daemon. It is the macos-port predicate check.
set -eu

usage() {
	printf 'usage: verify-nested-stack.sh [--pid PID] [expected-child-cmd ...]\n' >&2
	exit 2
}

pid=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		--pid)
			[ "$#" -ge 2 ] || usage
			pid=$2
			shift 2
			;;
		-h|--help)
			usage
			;;
		--)
			shift
			break
			;;
		-*)
			usage
			;;
		*)
			break
			;;
	esac
done

if [ -z "$pid" ]; then
	pid=$(pgrep -f '[d]evctl daemon' | head -n 1 || true)
fi
if [ -z "$pid" ]; then
	printf 'verify-nested-stack: no devctl daemon process\n' >&2
	exit 1
fi

if ! kill -0 "$pid" 2>/dev/null; then
	printf 'verify-nested-stack: pid %s is not running\n' "$pid" >&2
	exit 1
fi

# Direct children of the daemon (nested requirement).
children=$(pgrep -P "$pid" || true)

printf 'daemon pid %s\n' "$pid"
if [ -z "$children" ]; then
	printf 'verify-nested-stack: daemon has no child processes\n' >&2
	exit 1
fi
printf 'children %s\n' "$children"

# Walk the subtree for forbidden supervisors.
forbidden=0
# ps args differ: macOS ps -o command=, Linux ps -o args=.
ps_cmd() {
	ps -p "$1" -o command= 2>/dev/null || ps -p "$1" -o args= 2>/dev/null || true
}

walk=""
walk="$pid $children"
for c in $children; do
	grand=$(pgrep -P "$c" || true)
	walk="$walk $grand"
done

for p in $walk; do
	cmd=$(ps_cmd "$p")
	printf 'proc %s %s\n' "$p" "$cmd"
	case "$cmd" in
		*brew*|*homebrew*)
			printf 'verify-nested-stack: homebrew in process tree: %s\n' "$cmd" >&2
			forbidden=1
			;;
		*systemctl*)
			printf 'verify-nested-stack: systemctl in child tree: %s\n' "$cmd" >&2
			forbidden=1
			;;
	esac
done

if [ "$forbidden" -ne 0 ]; then
	exit 1
fi

fail_missing=0
for want in "$@"; do
	found=0
	for p in $walk; do
		cmd=$(ps_cmd "$p")
		case "$cmd" in
			*"$want"*)
				found=1
				break
				;;
		esac
	done
	if [ "$found" -eq 0 ]; then
		printf 'verify-nested-stack: missing nested process matching %s\n' "$want" >&2
		fail_missing=1
	fi
done

if [ "$fail_missing" -ne 0 ]; then
	exit 1
fi

printf 'verify-nested-stack: ok\n'
exit 0
