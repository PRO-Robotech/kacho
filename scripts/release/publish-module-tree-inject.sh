#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# Test-only publisher protocol holder. No product CLI reads these fixture modes.
set -euo pipefail
case "${1:-}" in
  --fixture-forge|--fixture-transport)
    fixture_mode="$1"; shift
    exec python3 - "$fixture_mode" "$@" <<'PY'
from pathlib import Path
from http.server import HTTPServer, BaseHTTPRequestHandler
import hashlib, json, os, re, shutil, signal, subprocess, sys, time, urllib.parse

MODE, CONFIG = sys.argv[1:3]
C = json.loads(Path(CONFIG).read_text())
ROOT = Path(C['root']).resolve()
CAP = Path(C['captures']).resolve()
CAP.mkdir(parents=True, exist_ok=True)
GIT = shutil.which('git')
ENV = {k:v for k,v in os.environ.items() if not k.startswith('GIT_')}
ENV.update(GOWORK='off', GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null',
           GIT_TERMINAL_PROMPT='0', GIT_AUTHOR_NAME='CI-RS fixture',
           GIT_AUTHOR_EMAIL='ci-rs@invalid', GIT_COMMITTER_NAME='CI-RS fixture',
           GIT_COMMITTER_EMAIL='ci-rs@invalid')
serial = 0

def sha(raw): return hashlib.sha256(raw).hexdigest()
def save(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True)+'\n')
def run(args, cwd=ROOT, data=None, check=True):
    global serial
    serial += 1
    start = time.monotonic()
    p = subprocess.run([GIT]+args, cwd=cwd, env=ENV, input=data, capture_output=True, timeout=20)
    prefix = CAP / ('git-%05d'%serial)
    Path(str(prefix)+'.stdout').write_bytes(p.stdout)
    Path(str(prefix)+'.stderr').write_bytes(p.stderr)
    save(str(prefix)+'.json', {'command':[GIT]+args,'cwd':str(cwd),'exit_code':p.returncode,
        'stdin_sha256':sha(data or b''),'stdout_sha256':sha(p.stdout),'stderr_sha256':sha(p.stderr),
        'seconds':time.monotonic()-start})
    if check and p.returncode: raise RuntimeError('actual Git prerequisite failed: '+p.stderr.decode())
    return p

def ref(repo, name):
    p=run(['--git-dir',str(repo),'rev-parse','--verify',name],check=False)
    if p.returncode == 0: return p.stdout.decode().strip()
    if p.returncode == 128: return None
    raise RuntimeError('actual Git ref read unavailable')

def install_transport(repo, mode, expected_ref=None, expected_sha=None):
    repo=Path(repo).resolve()
    if not repo.is_relative_to(ROOT) or not (repo/'HEAD').is_file():
        raise RuntimeError('transport endpoint is not an owned bare fixture')
    receive=Path(run(['--exec-path']).stdout.decode().strip())/'git-receive-pack'
    wrapper=ROOT/(repo.name+'-receive-pack')
    wrapper.write_text('#!/bin/sh\nset -eu\n[ "$#" -eq 1 ] && [ "$1" = '+repr(str(repo))+' ] || exit 125\nunset GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT\nexec '+repr(str(receive))+' "$@"\n')
    wrapper.chmod(0o755)
    hook_capture=CAP/(repo.name+'-hook.json')
    hook = '''#!/usr/bin/python3
from pathlib import Path
import json,os,signal,subprocess,sys
expected_root,receive,capture,mode,expected_ref,expected_sha,reset_sha=%r
parent=os.getppid();proc=Path('/proc')/str(parent)
argv=[x.decode() for x in (proc/'cmdline').read_bytes().split(b'\\0') if x]
exe=os.readlink(proc/'exe');root=subprocess.check_output(['/usr/bin/git','rev-parse','--absolute-git-dir'],text=True).strip()
updates=sys.stdin.read().strip().splitlines()
parts=updates[0].split() if len(updates)==1 else []
verified=(os.path.samefile(exe,receive) and len(argv)==2 and Path(argv[0]).name=='git-receive-pack' and Path(argv[1]).resolve()==Path(expected_root).resolve() and Path(root).resolve()==Path(expected_root).resolve() and len(parts)==3 and parts[0]=='0'*40 and len(parts[1])==40 and (parts[2].startswith('refs/heads/release/module-') or (expected_ref is not None and parts[2].startswith('refs/tags/v'))))
if expected_ref is not None: verified=verified and parts[2]==expected_ref
if expected_sha is not None: verified=verified and parts[1]==expected_sha
record={'hook_pid':os.getpid(),'receive_pack_pid':parent,'receive_pack_command':argv,'receive_pack_executable':exe,'git_root':root,'updates':updates,'mode':mode,'verified_owned_receive_pack':verified}
if (mode.startswith('lost') or mode=='post-tag-reset') and verified:
 observed=subprocess.check_output(['/usr/bin/git','rev-parse',parts[2]],text=True).strip();record['observed_after_update']=observed;verified=observed==parts[1];record['verified_owned_receive_pack']=verified
with open(capture,'w') as f:json.dump(record,f,indent=2);f.write('\\n');f.flush();os.fsync(f.fileno())
if not verified:sys.exit(125)
if mode=='reject':sys.exit(1)
if mode=='post-tag-reset':
 subprocess.run(['/usr/bin/git','update-ref','refs/heads/main',reset_sha],check=True);sys.exit(0)
if mode=='lost-unavailable':os.rename(expected_root,expected_root+'.hidden')
os.kill(parent,signal.SIGKILL)
sys.exit(0)
'''%((str(repo),str(receive),str(hook_capture),mode,expected_ref,expected_sha,C.get('base')),)
    for name in ('pre-receive','post-receive'):
        p=repo/'hooks'/name
        if p.exists(): p.unlink()
    if mode!='lawful':
        p=repo/'hooks'/('pre-receive' if mode=='reject' else 'post-receive')
        p.write_text(hook);p.chmod(0o755)
    (CAP/(repo.name+'-receive-pack.sh')).write_bytes(wrapper.read_bytes())
    if mode!='lawful':(CAP/(repo.name+'-server-hook.py')).write_bytes(p.read_bytes())
    save(CAP/(repo.name+'-transport.json'),{'mode':mode,'wrapper':str(wrapper),'wrapper_sha256':sha(wrapper.read_bytes()),'receive_pack':str(receive),'endpoint':str(repo),'hook_capture':str(hook_capture)})
    return str(wrapper)

if MODE=='--fixture-transport':
    wrapper=install_transport(C['repository'], C['transport_mode'], C.get('expected_ref'), C.get('expected_sha'))
    print(json.dumps({'wrapper':wrapper}));sys.exit(0)

if MODE!='--fixture-forge': raise RuntimeError('unknown test-only mode')
REPO=Path(C['repository']).resolve()
if not REPO.is_relative_to(ROOT): raise RuntimeError('foreign receiving endpoint')
CONTROL=Path(C['control']); STATE=Path(C['state'])
state={'prs':[],'notes':[],'writes':[],'reads':{},'unknown':[],'faults_used':{},'requests':0}
BASE='/repos/PRO-Robotech/corelib'
def controls(): return json.loads(CONTROL.read_text())
def persist(): save(STATE,state)
def current(): return ref(REPO,'refs/heads/main')
def bare(args, data=None, check=True): return run(['--git-dir',str(REPO)]+args,data=data,check=check)
def pr_view(pr):
    p=dict(pr);p['head']={'ref':pr['head_ref'],'sha':ref(REPO,'refs/heads/'+pr['head_ref']),'repo':{'full_name':'PRO-Robotech/corelib'}}
    p['base']={'ref':'main','sha':current(),'repo':{'full_name':'PRO-Robotech/corelib'}}
    p['html_url']='https://github.com/PRO-Robotech/corelib/pull/'+str(pr['number'])
    p['mergeable']=True;p['mergeable_state']='clean';return p

def actual_merge(pr):
    head=ref(REPO,'refs/heads/'+pr['head_ref']); old=current()
    tree=bare(['rev-parse',(C['base'] if controls().get('fault')=='merge-content-mismatch' else head)+'^{tree}']).stdout.decode().strip()
    commit=bare(['commit-tree',tree,'-p',old,'-p',head],b'Actual CI-RS fixture merge\n').stdout.decode().strip()
    bare(['update-ref','refs/heads/main',commit,old])
    pr['merged']=True;pr['state']='closed';pr['merge_commit_sha']=commit
    if controls().get('fault')=='merge-ancestry-lost':bare(['update-ref','refs/heads/main',C['base'],commit])
    return commit

def controlled_mutation(pr):
    mutation=controls().get('mutation')
    if not mutation:return
    if mutation.get('trigger')!='after-pr-create' or state['faults_used'].get('mutation'):raise RuntimeError('unexpected mutation trigger')
    marker='CI-RS-1 plan-sha256:'+mutation['accepted_plan']
    if marker not in pr['body']:raise RuntimeError('mutation lacks already-effectful accepted plan marker')
    record={'trigger':'after actual branch push and PR creation, before PR response','pr_number':pr['number'],'accepted_plan':mutation['accepted_plan'],'kind':mutation['kind']}
    if mutation['kind']=='file':
        path=Path(mutation['path']).resolve()
        if not path.is_relative_to(ROOT):raise RuntimeError('foreign mutation path')
        before=path.read_bytes()
        if sha(before)!=mutation['before_sha256']:raise RuntimeError('mutation source mismatch')
        after=mutation['after'].encode();path.write_bytes(after)
        record.update(path=str(path),before_sha256=sha(before),after_sha256=sha(after))
    elif mutation['kind']=='ref':
        name=mutation['ref'];old=ref(REPO,name)
        if old!=mutation['before_sha']:raise RuntimeError('mutation old ref mismatch')
        if mutation['operation']=='advance':
            tree=bare(['rev-parse',old+'^{tree}']).stdout.decode().strip()
            new=bare(['commit-tree',tree,'-p',old],b'after-admission fixture ref advancement\n').stdout.decode().strip()
        else:raise RuntimeError('unknown ref mutation')
        bare(['update-ref',name,new,old]);record.update(ref=name,before_sha=old,after_sha=ref(REPO,name))
    else:raise RuntimeError('unknown bounded mutation')
    state['faults_used']['mutation']=True;state['controlled_mutation']=record;save(CAP/'controlled-mutation.json',record)

class Handler(BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def do_GET(self): self.handle_request()
    def do_POST(self): self.handle_request()
    def do_PUT(self): self.handle_request()
    def do_PATCH(self): self.handle_request()
    def do_DELETE(self): self.handle_request()
    def answer(self,status,body,record,drop=False):
        raw=body if isinstance(body,bytes) else json.dumps(body,separators=(',',':')).encode()
        record.update(status=status,response_sha256=sha(raw),dropped_after_effect=drop)
        name=CAP/('http-%05d'%state['requests'])
        Path(str(name)+'.response').write_bytes(raw);save(str(name)+'.json',record);persist()
        if drop:
            self.close_connection=True
            try:self.connection.shutdown(2)
            except OSError:pass
            self.connection.close();return
        self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
    def handle_request(self):
        try:self.serve_fixture()
        except Exception as error:
            state['unknown'].append('fixture failure: '+type(error).__name__+': '+str(error));persist();raise
    def serve_fixture(self):
        state['requests']+=1
        original=self.headers.get('X-CI-RS-Original-URL','')
        u=urllib.parse.urlsplit(original);path=urllib.parse.unquote(u.path);query=urllib.parse.parse_qs(u.query)
        raw=self.rfile.read(int(self.headers.get('Content-Length','0')))
        record={'method':self.command,'url':original,'request_sha256':sha(raw)}
        (CAP/('http-%05d.request'%state['requests'])).write_bytes(raw)
        try:body=json.loads(raw) if raw else {}
        except ValueError:body={}
        ctrl=controls();fault=ctrl.get('fault','lawful');method=self.command
        key=method+' '+path;state['reads'][key]=state['reads'].get(key,0)+1
        if u.scheme!='https' or u.hostname not in ('api.github.com','proxy.golang.org'):
            state['unknown'].append(key);return self.answer(599,{'fixture_error':'undeclared endpoint'},record)
        # Exact captured prior review/authorization and fixed old-version proxy.
        for route in C['http']:
            if route['method']==method and route['url']==original:
                b=Path(route['body_path']).read_bytes()
                if sha(b)!=route['body_sha256']:raise RuntimeError('fixture HTTP source drift')
                return self.answer(route['status'],b,record)
        if u.hostname=='proxy.golang.org':
            if fault=='proxy-unavailable':return self.answer(503,{'message':'fixture unavailable'},record)
            suffix=path.rsplit('/',1)[-1]
            mapped=C['published'].get(suffix)
            if mapped:
                b=Path(mapped).read_bytes()
                if fault=='published-archive-mismatch' and suffix.endswith('.zip'):b=Path(C['wrong_zip']).read_bytes()
                if fault=='published-payload-mismatch' and suffix.endswith('.zip'):b=Path(C['wrong_payload_zip']).read_bytes()
                return self.answer(200,b,record)
            return self.answer(404,{'message':'not found'},record)
        if path==BASE:
            if fault=='read-unavailable':return self.answer(503,{'message':'fixture unavailable'},record)
            if fault=='read-recovers' and state['reads'][key]<3:return self.answer(503,{'message':'transient fixture'},record)
            return self.answer(200,{'full_name':'PRO-Robotech/corelib','default_branch':'main','permissions':{'pull':True,'push':True,'admin':False}},record)
        if path==BASE+'/branches/main/protection':
            return self.answer(200,{'required_status_checks':{'strict':True,'contexts':['fixture-required'],'checks':[{'context':'fixture-required','app_id':1}]},'required_pull_request_reviews':{'required_approving_review_count':1},'enforce_admins':{'enabled':True},'allow_force_pushes':{'enabled':False}},record)
        if path==BASE+'/branches/main':return self.answer(200,{'name':'main','protected':True,'commit':{'sha':current()}},record)
        if re.fullmatch(re.escape(BASE)+r'/commits/[0-9a-f]{40}/(check-runs|status)',path):
            commit=path.split('/')[-2];bad=fault=='checks-failed';pending=fault=='checks-pending'
            if fault=='pr-head-changed' and state['prs'] and not state['faults_used'].get(fault):
                pr=state['prs'][0];name='refs/heads/'+pr['head_ref'];old=ref(REPO,name);tree=bare(['rev-parse',old+'^{tree}']).stdout.decode().strip();new=bare(['commit-tree',tree,'-p',old],b'changed PR head fixture\n').stdout.decode().strip();bare(['update-ref',name,new,old]);state['head_mutation']={'ref':name,'before_sha':old,'after_sha':new,'trigger':'required-check read'};state['faults_used'][fault]=True
            if fault=='checks-last-sleep' and state['reads'][key]<2:pending=True
            if path.endswith('/status'):return self.answer(200,{'sha':commit,'state':'pending' if pending else 'failure' if bad else 'success','statuses':[{'context':'fixture-required','state':'pending' if pending else 'failure' if bad else 'success'}]},record)
            return self.answer(200,{'total_count':1,'check_runs':[{'id':41,'name':'fixture-required','head_sha':commit,'status':'in_progress' if pending else 'completed','conclusion':None if pending else 'failure' if bad else 'success','app':{'id':1}}]},record)
        if path.startswith(BASE+'/git/ref/') and method=='GET':
            name='refs/'+path.split('/git/ref/',1)[1];observed=ref(REPO,name)
            if observed is None:return self.answer(404,{'message':'not found'},record)
            return self.answer(200,{'ref':name,'object':{'type':'commit','sha':observed}},record)
        if path==BASE+'/pulls' and method=='GET':
            if fault in ('pr-lost-unavailable','merge-lost-unavailable') and state['prs'] and (fault=='pr-lost-unavailable' or state['prs'][0]['merged']):return self.answer(503,{'message':'readback unavailable'},record)
            if fault=='pr-lost-empty' and state['prs']:return self.answer(200,[],record)
            prs=[pr_view(p) for p in state['prs']]
            if fault=='pr-duplicate' and prs:prs.append(dict(prs[0],number=102))
            return self.answer(200,prs,record)
        if path==BASE+'/pulls' and method=='POST':
            state['writes'].append({'kind':'pr','method':method,'body':body})
            if fault=='pr-rejected':return self.answer(422,{'message':'terminal fixture rejection'},record)
            head=body.get('head','').split(':')[-1]
            if body.get('base')!='main' or not head.startswith('release/module-') or ref(REPO,'refs/heads/'+head) is None:
                raise RuntimeError('fixture PR request does not bind actual pushed branch')
            pr={'number':101,'head_ref':head,'created_head_sha':ref(REPO,'refs/heads/'+head),'body':body.get('body',''),'title':body.get('title',''),'state':'open','merged':False,'merge_commit_sha':None}
            state['prs'].append(pr)
            controlled_mutation(pr)
            return self.answer(201,pr_view(pr),record,fault in ('pr-lost-present','pr-lost-unavailable','pr-lost-empty','pr-duplicate'))
        match=re.fullmatch(re.escape(BASE)+r'/pulls/(\d+)(/merge)?',path)
        if match:
            pr=next((p for p in state['prs'] if p['number']==int(match[1])),None)
            if pr is None:return self.answer(404,{'message':'not found'},record)
            if method=='GET':
                if fault=='merge-lost-unavailable' and pr['merged']:return self.answer(503,{'message':'readback unavailable'},record)
                return self.answer(200,pr_view(pr),record)
            if method=='PUT' and match[2]:
                state['writes'].append({'kind':'merge','method':method,'body':body})
                if fault=='merge-rejected':return self.answer(405,{'merged':False,'message':'terminal rejection'},record)
                if body.get('sha')!=ref(REPO,'refs/heads/'+pr['head_ref']):return self.answer(409,{'message':'head changed'},record)
                if fault=='merge-head-conflict':
                    name='refs/heads/'+pr['head_ref'];old=ref(REPO,name);tree=bare(['rev-parse',old+'^{tree}']).stdout.decode().strip();new=bare(['commit-tree',tree,'-p',old],b'changed at merge write fixture\n').stdout.decode().strip();bare(['update-ref',name,new,old]);state['head_mutation']={'ref':name,'before_sha':old,'after_sha':new,'trigger':'merge write'};return self.answer(409,{'message':'head changed'},record)
                commit=actual_merge(pr)
                return self.answer(200,{'sha':commit,'merged':True,'message':'merged'},record,fault.startswith('merge-lost'))
        if path.startswith(BASE+'/releases/tags/') and method=='GET':
            tag=path.split('/releases/tags/',1)[1];notes=[n for n in state['notes'] if n['tag_name']==tag]
            if fault=='note-conflict' and notes:notes=[dict(notes[0],target_commitish=C['base'])]
            if fault=='note-lost-unavailable' and notes:return self.answer(503,{'message':'unavailable'},record)
            return self.answer(200 if notes else 404,notes[0] if notes else {'message':'not found'},record)
        if path==BASE+'/releases' and method=='GET':
            if fault=='note-lost-unavailable' and state['notes']:return self.answer(503,{'message':'unavailable'},record)
            notes=state['notes']
            if fault=='note-conflict' and notes:notes=[dict(notes[0],target_commitish=C['base'])]
            return self.answer(200,notes,record)
        if path==BASE+'/releases' and method=='POST':
            state['writes'].append({'kind':'release-note','method':method,'body':body})
            target=ref(REPO,'refs/tags/'+body.get('tag_name',''))
            if target is None:raise RuntimeError('release note before actual tag')
            note=dict(body,id=201,target_commitish=target);state['notes'].append(note)
            return self.answer(201,note,record,fault.startswith('note-lost'))
        state['unknown'].append(key);return self.answer(599,{'fixture_error':'unhandled fixture route'},record)

server=HTTPServer(('127.0.0.1',0),Handler)
persist();print(json.dumps({'endpoint':'http://127.0.0.1:'+str(server.server_address[1]),'pid':os.getpid()}),flush=True)
server.serve_forever(poll_interval=.05)
PY
    ;;
  "")
    cd "$(dirname "$0")/../.."
    export GOWORK=off
    exec go test ./internal/release -run '^TestReleaseSupplyPublisherProtocol$' -count=1 -json -timeout=15m
    ;;
  *) printf '%s\n' 'test-only holder accepts no product arguments' >&2; exit 2 ;;
esac
