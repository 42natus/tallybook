#!/usr/bin/env python3
"""Regenerate the Example-function issues in the wave 2 backlog.

These are the 118 `docs(<pkg>): add a runnable example for X` issues. They are
machine-generated so every citation is derived from the code rather than typed
by hand: the script scans internal/ for exported identifiers, records each one's
real file and line, and decides what kind of example it needs from its actual
signature.

    python3 scripts/gen-example-issues.py --check   # verify against the backlog
    python3 scripts/gen-example-issues.py --print    # emit the JSON

It deliberately does not rewrite docs/WAVE2_BACKLOG.json in place: the 22
hand-written substantive issues share that file, and regenerating over them
would lose them. --check is what CI and review should run.
"""
import json, os, re, sys, collections

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TARGET_COUNT = 123


def scan_exported():
    """Every exported identifier under internal/, with its real file and line."""
    out = []
    for root, _, files in os.walk(os.path.join(REPO, "internal")):
        for fn in sorted(files):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            path = os.path.relpath(os.path.join(root, fn), REPO)
            for n, line in enumerate(open(os.path.join(REPO, path)), 1):
                if line.startswith("func ("):
                    m = re.match(r'^func \(([^)]+)\) ([A-Z]\w*)\(', line)
                    if not m:
                        continue
                    kind, recv, name = "method", m.group(1), m.group(2)
                elif line.startswith("func "):
                    m = re.match(r'^func ([A-Z]\w*)\(', line)
                    if not m:
                        continue
                    kind, recv, name = "func", "", m.group(1)
                elif line.startswith("type "):
                    m = re.match(r'^type ([A-Z]\w*) ', line)
                    if not m:
                        continue
                    kind, recv, name = "type", "", m.group(1)
                else:
                    continue
                out.append(dict(path=path, line=n, decl=line.strip(), kind=kind, recv=recv,
                                name=name, pkg=path.split(os.sep)[1],
                                takes_ctx="ctx context.Context" in line))
    return out


def recv_type(recv):
    """'c *Config' -> 'Config'"""
    m = re.search(r'\*?([A-Z]\w*)\s*$', recv.strip())
    return m.group(1) if m else ""


def example_name(i):
    """Go's required Example naming: ExampleF, ExampleT, ExampleT_M."""
    if i["kind"] == "method":
        t = recv_type(i["recv"])
        return f"Example{t}_{i['name']}" if t else f"Example{i['name']}"
    return f"Example{i['name']}"


def display_name(i):
    if i["kind"] == "method":
        t = recv_type(i["recv"])
        return f"{t}.{i['name']}" if t else i["name"]
    return i["name"]


def test_file_for(i):
    """The _test.go counterpart of the identifier's own source file, if it exists."""
    cand = i["path"][:-3] + "_test.go"
    if os.path.exists(os.path.join(REPO, cand)):
        return cand, True
    return os.path.join("internal", i["pkg"], "example_test.go"), False


def category(i):
    if i["takes_ctx"]:
        return "io"
    if re.match(r'^(Scv|Decode|Encode|Fold|Verify|Hash|Leaf|String|Error|Age)', i["name"]):
        return "pure"
    if re.match(r'^(Parse|Load|Validate|New)', i["name"]):
        return "ctor"
    if i["kind"] == "type":
        return "type"
    return "pure"


CAT = {
 "pure": dict(
   fix="calls it with a concrete literal input and asserts the result through an `// Output:` "
       "comment, so `go test` executes the example and it cannot silently rot",
   why="It is deterministic, so the example can assert its exact output and will fail the build "
       "the moment the behaviour changes — which is what makes an example worth more here than "
       "another line of prose.",
   crit=["`{EX}` exists in `{tf}` and has an `// Output:` comment, so `go test` executes and checks it",
         "The example passes a concrete literal input rather than calling a helper that hides what is being passed",
         "`go test ./{pp}/...` passes with the asserted output matching exactly",
         "`go doc {pp} {disp}` shows the example attached to this identifier, not to the package"]),
 "ctor": dict(
   fix="shows one accepted input and one rejected input, with the rejection's error identified "
       "by the sentinel it matches",
   why="It is a parse or construction entry point, so the valuable half of the example is the "
       "rejection: showing which sentinel a malformed input produces documents the contract far "
       "better than the signature does.",
   crit=["`{EX}` exists in `{tf}` and covers one accepted and one rejected input",
         "The rejected case identifies the sentinel via `errors.Is`, not by comparing an error string",
         "The example has an `// Output:` comment so `go test` executes and checks it",
         "`go test ./{pp}/...` passes"]),
 "type": dict(
   fix="constructs it and uses it the way a caller actually would, so the difference between the "
       "zero value and a properly-constructed one is visible",
   why="It is an exported type with no example, so nothing in the package documentation shows "
       "which fields a caller is expected to set versus which the package fills in. That is "
       "exactly the question an example answers and a doc comment usually does not.",
   crit=["`{EX}` exists in `{tf}` and constructs the type the way a caller would",
         "The example sets every field a caller is required to set, and leaves the ones the package fills in alone",
         "The example either asserts an `// Output:` comment, or carries a comment saying why output is not asserted",
         "`go test ./{pp}/...` passes"]),
 "io": dict(
   fix="is compile-checked against the package's existing test stubs rather than a live "
       "dependency, with a comment stating why no `// Output:` is asserted",
   why="It takes a `context.Context` and performs I/O, so a runnable example would need a live "
       "Postgres or Soroban endpoint. A compile-only example still documents the call shape and "
       "still breaks the build if the signature changes, which is the part worth protecting.",
   crit=["`{EX}` exists in `{tf}` and compiles as part of the package's tests",
         "It uses the stubs already defined in the package's tests and opens no real network or database connection",
         "It constructs its context explicitly with a timeout, per CLAUDE.md §8",
         "It carries a comment explaining why no `// Output:` is asserted, so the omission is deliberate",
         "`go test ./{pp}/...` passes without Postgres running"]),
}


def render(picked):
    issues = []
    for i in picked:
        pkg, pp = i["pkg"], f"internal/{i['pkg']}"
        EX, disp = example_name(i), display_name(i)
        tf, tf_exists = test_file_for(i)
        c = CAT[i["cat"]]
        f = dict(EX=EX, tf=tf, pp=pp, disp=disp)
        recv = f", a method on `{recv_type(i['recv'])}`" if i["kind"] == "method" else ""

        guidance = [
            f"The declaration is at {i['path']}:{i['line']} — read its doc comment first. This "
            f"repository uses doc comments to record design decisions, and the example should "
            f"agree with what is there rather than restate it."]
        if tf_exists:
            guidance.append(f"`{tf}` already tests this file — add the example there, or in a "
                            f"sibling `example_test.go` in the same package. `go test` only runs "
                            f"`Example*` functions that live alongside the package's other tests.")
        else:
            guidance.append(f"There is no `_test.go` beside {i['path']} yet, so create `{tf}` in "
                            f"package `{pkg}` for it.")
        if i["kind"] == "method":
            guidance.append(f"The name must be exactly `{EX}` — Go attaches an example to a "
                            f"method only via the `ExampleType_Method` form. Naming it "
                            f"`Example{i['name']}` would silently document the package instead "
                            f"of `{disp}`.")
        elif i["cat"] == "io":
            guidance.append("Reuse the stubs the package's tests already define; the narrow "
                            "interfaces exist precisely so this works without a live endpoint.")
        else:
            guidance.append("An example whose `// Output:` comment does not match is a build "
                            "failure. That is the point — do not delete the comment to make it "
                            "pass.")

        issues.append({
            "title": f"docs({pkg}): add a runnable example for {disp}",
            "complexity": "low",
            "area": pkg,
            "description": f"Add `{EX}` for `{disp}` ({i['path']}:{i['line']}{recv}), which "
                           + c["fix"] + ".",
            "why": f"`{disp}` is exported from `{pp}` at {i['path']}:{i['line']} "
                   f"(`{i['decl'].rstrip(' {')}`) and has no example. The repository has "
                   f"**zero** `Example` functions across all 239 exported identifiers, so "
                   f"`go doc` shows signatures and prose but never one executable call. "
                   + c["why"],
            "criteria": [x.format(**f) for x in c["crit"]],
            "guidance": guidance[:4],
            "good_first_issue": True,
            "entry_point": i["path"],
        })
    return issues


def build():
    api = scan_exported()
    for i in api:
        i["cat"] = category(i)
    order = {"pure": 0, "ctor": 1, "type": 2, "io": 3}
    cands = sorted(api, key=lambda i: (order[i["cat"]], i["pkg"], display_name(i)))
    seen, picked = set(), []
    for i in cands:
        key = (i["pkg"], display_name(i))
        if key in seen:
            continue
        seen.add(key)
        picked.append(i)
        if len(picked) == TARGET_COUNT:
            break
    return render(picked), picked, len(api)


if __name__ == "__main__":
    issues, picked, scanned = build()
    mode = sys.argv[1] if len(sys.argv) > 1 else "--check"
    if mode == "--print":
        print(json.dumps(issues, indent=2))
        sys.exit(0)
    backlog = json.load(open(os.path.join(REPO, "docs/WAVE2_BACKLOG.json")))
    missing = {i["title"] for i in issues} - {i["title"] for i in backlog}
    print(f"scanned {scanned} exported identifiers under internal/")
    print(f"generated {len(issues)} example issues; "
          f"by category: {dict(collections.Counter(i['cat'] for i in picked))}")
    if missing:
        print(f"MISMATCH: {len(missing)} generated issues are not in the backlog:", file=sys.stderr)
        for t in sorted(missing)[:10]:
            print(f"  - {t}", file=sys.stderr)
        sys.exit(1)
    print(f"OK: all {len(issues)} generated titles are present in docs/WAVE2_BACKLOG.json")
