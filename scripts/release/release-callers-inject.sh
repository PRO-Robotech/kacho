#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# Independent test driver. None of these environment names is a product seam.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

# This native Git boundary is shared by the additive caller holder and the
# fixture-only historical successor. It never substitutes Go or a result.
if [[ "${1:-}" == --git-adapter ]]; then
    [[ $# == 4 ]] || exit 2
    dest=$2; owned=$3; capture=$4
    realgit=$(command -v git); python=$(command -v python3)
    mkdir -p "$dest" "$capture"
    "$python" - "$dest/config.json" "$realgit" "$owned" "$capture" <<'PY'
import json,os,sys
p,git,root,capture=sys.argv[1:]
assert all(os.path.isabs(x) for x in (p,git,root,capture))
json.dump(dict(git=git,root=os.path.realpath(root),capture=capture),open(p,'w'))
PY
    printf '#!%s\n' "$python" > "$dest/git"
    cat >> "$dest/git" <<'PY'
import hashlib,json,os,pathlib,subprocess,sys,time,uuid
cfg=json.load(open(pathlib.Path(__file__).with_name('config.json')))
args=sys.argv[1:]
# The frozen d38 global-option policy: option values cannot become verbs.
def index(a):
    takes={'-C','-c','--git-dir','--work-tree','--namespace','--super-prefix','--config-env','--attr-source'}
    flags={'-p','--paginate','-P','--no-pager','--bare','--no-replace-objects','--literal-pathspecs','--glob-pathspecs','--noglob-pathspecs','--icase-pathspecs','--no-optional-locks','--no-lazy-fetch','--no-advice'}
    equals=('--git-dir=','--work-tree=','--namespace=','--super-prefix=','--config-env=','--attr-source=','--exec-path=')
    i=0
    while i<len(a):
        s=a[i]
        if not s.startswith('-'): return i
        if s in takes:
            if i+1>=len(a): return -1
            i+=2;continue
        if s in flags or s.startswith(equals): i+=1;continue
        return -1
    return -1
i=index(args);verb=args[i] if i>=0 else ''
prefix=['-c','protocol.allow=never','-c','protocol.file.allow=always']
mapping=None
if verb in ('clone','fetch','ls-remote','push'):
    # Per-fixture local config is test data; no production program reads it.
    q=subprocess.run([cfg['git'],*args[:i],'config','--get','ciRsFixture.origin'],capture_output=True)
    local=q.stdout.decode().strip() if q.returncode==0 else ''
    if local:
        real=os.path.realpath(local)
        if os.path.commonpath([cfg['root'],real])!=cfg['root'] or not pathlib.Path(real,'HEAD').is_file():
            raise SystemExit('HARNESS_NOT_EXECUTED: foreign fixture endpoint')
        canonical='https://github.com/PRO-Robotech/kacho.git'
        mapping=dict(canonical=canonical,local='file://'+real)
        prefix+=['-c','url.'+mapping['local']+'.insteadOf='+canonical]
actual=prefix+args
started=time.monotonic()
# Preserve inherited environment: sanitation is the caller's responsibility.
p=subprocess.run([cfg['git'],*actual],stdin=sys.stdin.buffer,capture_output=True)
record=dict(original_args=args,actual_args=actual,cwd=os.getcwd(),mapping=mapping,
            verb=verb,exit_code=p.returncode,seconds=time.monotonic()-started,
            git_environment_keys=sorted(k for k in os.environ if k.startswith('GIT_')),
            stdout_sha256=hashlib.sha256(p.stdout).hexdigest(),stderr_sha256=hashlib.sha256(p.stderr).hexdigest())
stem=pathlib.Path(cfg['capture'],str(time.time_ns())+'-'+uuid.uuid4().hex)
stem.with_suffix('.json').write_text(json.dumps(record,sort_keys=True)+'\n')
stem.with_suffix('.stdout').write_bytes(p.stdout);stem.with_suffix('.stderr').write_bytes(p.stderr)
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr)
raise SystemExit(p.returncode if p.returncode>=0 else 128-p.returncode)
PY
    chmod 755 "$dest/git"
    exit 0
fi

[[ $# == 0 ]] || { echo 'usage: release-callers-inject.sh' >&2; exit 2; }
while IFS='=' read -r key _; do [[ "$key" != GIT_* ]] || unset "$key"; done < <(env)
cd "$ROOT"
export GOWORK=off GOTOOLCHAIN=local
exec go test ./internal/release -run '^TestReleaseSupply(CLI|P9Caller|LegacyP1P8)$' -count=1 -json -timeout=35m
