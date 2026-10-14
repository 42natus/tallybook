#!/usr/bin/env bash
# Verify the wave backlog programmatically. Exits non-zero on any failure.
set -uo pipefail
cd "$(dirname "$0")/.."
BACKLOG="docs/WAVE2_BACKLOG.json"
fail=0
chk() { if [ "$2" = "$3" ]; then echo "  PASS  $1 ($2)"; else echo "  FAIL  $1: expected $3, got $2"; fail=1; fi; }

echo "=== 1. counts per complexity tier and exact point total ==="
python3 - "$BACKLOG" <<'PY'
import json, sys, collections
P = {"high": 200, "medium": 150, "low": 100}
iss = json.load(open(sys.argv[1]))
t = collections.Counter(i["complexity"] for i in iss)
for tier in ("high", "medium", "low"):
    print(f"  {tier:<7} x {t.get(tier,0):>3}  @ {P[tier]:>3} = {t.get(tier,0)*P[tier]:>6}")
print(f"  {'TOTAL':<7} x {len(iss):>3}        = {sum(P[i['complexity']] for i in iss):>6}")
PY

echo
echo "=== 2. no duplicate titles within the backlog ==="
dupes=$(jq -r '.[].title' "$BACKLOG" | sort | uniq -d | wc -l | tr -d ' ')
chk "duplicate titles inside backlog" "$dupes" "0"

echo
echo "=== 3. zero intersection with existing issue titles (both repos) ==="
jq -r '.[].title' "$BACKLOG" | sort > /tmp/wave_want.txt
gh issue list --repo Tallybook-Org/tallybook           --state all --limit 1000 --json title -q '.[].title' > /tmp/wave_live.txt
gh issue list --repo Tallybook-Org/tallybook-contracts --state all --limit 1000 --json title -q '.[].title' >> /tmp/wave_live.txt
sort -u /tmp/wave_live.txt -o /tmp/wave_live.txt
echo "  existing titles across both repos: $(wc -l < /tmp/wave_live.txt | tr -d ' ')"
overlap=$(comm -12 /tmp/wave_want.txt /tmp/wave_live.txt | wc -l | tr -d ' ')
chk "titles colliding with an existing issue" "$overlap" "0"
[ "$overlap" -ne 0 ] && comm -12 /tmp/wave_want.txt /tmp/wave_live.txt

echo
echo "=== 4. required fields present and in range ==="
python3 - "$BACKLOG" <<'PY'
import json, sys, os
iss = json.load(open(sys.argv[1]))
bad = 0
for n, i in enumerate(iss, 1):
    for f in ("title","complexity","area","description","why","criteria","guidance"):
        if not i.get(f): print(f"  FAIL  issue {n}: missing {f}"); bad += 1
    if not 3 <= len(i.get("criteria",[])) <= 5: print(f"  FAIL  issue {n}: {len(i['criteria'])} criteria"); bad += 1
    if not 2 <= len(i.get("guidance",[])) <= 4: print(f"  FAIL  issue {n}: {len(i['guidance'])} guidance"); bad += 1
print(f"  PASS  all {len(iss)} issues have every required field, criteria 3-5, guidance 2-4" if not bad
      else f"  {bad} field problems")
sys.exit(1 if bad else 0)
PY
[ $? -ne 0 ] && fail=1

echo
echo "=== 5. good_first_issue only on trivial/low, each with a real entry point ==="
python3 - "$BACKLOG" <<'PY'
import json, sys, os
iss = json.load(open(sys.argv[1])); bad = 0
gfi = [i for i in iss if i.get("good_first_issue")]
for i in gfi:
    if i["complexity"] != "low":
        print(f"  FAIL  '{i['title'][:50]}' is good_first_issue but complexity {i['complexity']}"); bad += 1
    ep = i.get("entry_point")
    if not ep: print(f"  FAIL  '{i['title'][:50]}' has no entry_point"); bad += 1
    elif not os.path.exists(ep): print(f"  FAIL  entry_point does not exist: {ep}"); bad += 1
print(f"  PASS  {len(gfi)} good-first-issues, all complexity:low, all entry points exist" if not bad else "")
sys.exit(1 if bad else 0)
PY
[ $? -ne 0 ] && fail=1

echo
echo "=== 6. every cited file:line in every issue body resolves ==="
python3 - "$BACKLOG" <<'PY'
import json, sys, re, os
iss = json.load(open(sys.argv[1]))
pat = re.compile(r'\b((?:cmd|internal|scripts|docs|\.github)/[\w./-]+\.(?:go|sql|yml|md)):(\d+)\b')
checked = bad = 0
for i in iss:
    for text in [i["why"], i["description"]] + i["criteria"] + i["guidance"]:
        for path, line in pat.findall(text):
            checked += 1
            if not os.path.exists(path):
                print(f"  FAIL  {path} does not exist"); bad += 1; continue
            n = sum(1 for _ in open(path))
            if int(line) > n:
                print(f"  FAIL  {path}:{line} beyond EOF ({n} lines)"); bad += 1
print(f"  PASS  {checked} file:line citations checked, all resolve" if not bad else f"  {bad} of {checked} citations broken")
sys.exit(1 if bad else 0)
PY
[ $? -ne 0 ] && fail=1

echo
if [ "$fail" -eq 0 ]; then echo "ALL CHECKS PASSED"; else echo "SOME CHECKS FAILED"; fi
exit "$fail"
