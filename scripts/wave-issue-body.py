#!/usr/bin/env python3
"""Print one backlog issue's GitHub body. Usage: wave-issue-body.py <backlog.json> <index>

Reuses the generator's body() renderer so an issue body on GitHub and the
generated markdown can never drift apart.
"""
import json, sys, os, importlib.util

here = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("genwave", os.path.join(here, "gen-wave-backlog.py"))
gen = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gen)

if len(sys.argv) != 3:
    sys.exit(__doc__)
print(gen.body(json.load(open(sys.argv[1]))[int(sys.argv[2])]))
