#!/usr/bin/env sh
# T-945: print the next eligible task and status counts, derived from TASK_TRACKER.md.
#
# The tracker forbids a hand-kept summary table because it drifts; this derives the same answer
# from the task blocks themselves every time, so it cannot. A task is "done" if its block says
# `status: done` or its id appears backticked on a phase's `**Done (archived ...)**` line (the
# block itself then lives in docs/tracker-archive.md). The next task is the first `todo` block in
# file order whose `depends:` ids are all done.
#
# Usage: scripts/next-task.sh [TRACKER]   (default TASK_TRACKER.md). Exit 0 on success, 2 on a
# missing file. POSIX sh + awk, passes `shellcheck -s sh`. Tested by scripts/next-task_test.sh.
set -eu

tracker=${1:-TASK_TRACKER.md}
if [ ! -f "$tracker" ]; then
	echo "next-task: no such file: $tracker" >&2
	exit 2
fi

awk '
function ids(s, into,    id) {
	while (match(s, /T-[0-9]+/)) {
		id = substr(s, RSTART, RLENGTH)
		into[id] = 1
		s = substr(s, RSTART + RLENGTH)
	}
}
/^\*\*Done \(archived/ {
	s = $0
	while (match(s, /`T-[0-9]+`/)) {
		done[substr(s, RSTART + 1, RLENGTH - 2)] = 1
		archived++
		s = substr(s, RSTART + RLENGTH)
	}
	next
}
/^## / { cur = "" }
/^### T-[0-9]+/ {
	match($0, /T-[0-9]+/)
	cur = substr($0, RSTART, RLENGTH)
	n++
	order[n] = cur
	title[cur] = substr($0, 5)
	next
}
cur != "" && /^status:/ && !(cur in status) { status[cur] = $2 }
cur != "" && /^depends:/ && !(cur in deps) { deps[cur] = substr($0, 9) }
cur != "" && /^tier:/ && !(cur in tier) { tier[cur] = $2 }
END {
	for (i = 1; i <= n; i++) {
		t = order[i]
		if (status[t] == "done") done[t] = 1
		count[status[t]]++
	}
	d = archived + count["done"]
	for (i = 1; i <= n; i++) {
		t = order[i]
		if (status[t] != "todo") continue
		split("", need)
		ids(deps[t], need)
		ok = 1
		for (k in need) if (!(k in done)) ok = 0
		if (ok) { nxt = t; break }
	}
	if (nxt != "") {
		tr = (nxt in tier) ? tier[nxt] : "?"
		printf "next: %s (tier %s)\n", title[nxt], tr
	} else {
		print "next: none eligible"
	}
	printf "done: %d  todo: %d  in-progress: %d  blocked: %d\n", d, count["todo"] + 0, count["in-progress"] + 0, count["blocked"] + 0
	for (i = 1; i <= n; i++) {
		t = order[i]
		if (status[t] == "blocked" || status[t] == "in-progress") printf "%s: %s\n", status[t], title[t]
	}
}
' "$tracker"
