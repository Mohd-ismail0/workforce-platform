#!/usr/bin/env python3
"""Run local app roles with external secret files; never print credentials."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('action', choices=['migrate','seed','api','worker','test','build'])
p.add_argument('--env', default=str(Path.home()/'.config/workforce-platform/database.env'))
p.add_argument('--auth-env', default=str(Path.home()/'.config/workforce-platform/local-auth.env'))
a = p.parse_args()
env = os.environ.copy()
for file in [Path(a.env),Path(a.auth_env)]:
    if file.exists():
        if file.stat().st_mode & 0o077:
            raise SystemExit(f'Refusing readable-by-others secret file: {file}')
        for line in file.read_text().splitlines():
            if line.strip() and not line.startswith('#'):
                key,value=line.split('=',1)
                env[key]=value
local_go=Path.home()/'.local/toolchains/go/bin'
if local_go.exists(): env['PATH']=str(local_go)+os.pathsep+env.get('PATH','')
env.setdefault('HTTP_ADDR','127.0.0.1:8095')
env.setdefault('APP_ENV','development')
if a.action != 'migrate': env.pop('MIGRATION_DATABASE_URL',None)
if a.action == 'test' and not env.get('DATABASE_URL'): raise SystemExit('DATABASE_URL required: this test command must not silently skip PostgreSQL tests')
# TEST_MASTER_DATABASE_URL lets packages run against their OWN migrated
# throwaway database (see internal/testdb). It is optional: when absent or when
# the role lacks CREATEDB, tests fall back to the shared DATABASE_URL and the
# -p 1 serialisation below remains the correctness guarantee.
env.setdefault('TEST_MASTER_DATABASE_URL', env.get('MIGRATION_DATABASE_URL') or '')
commands={
 'migrate':['go','run','./cmd/workforce','-migrate'],
 'seed':['go','run','./cmd/workforce','-seed'],
 'api':['go','run','./cmd/workforce'],
 'worker':['go','run','./cmd/workforce','-worker'],
 # -p 1: packages run serially. The api tests start real River workers on the
 # shared database and queue, so parallel packages would steal each other's jobs.
 # This is only half the story: with TEST_MASTER_DATABASE_URL set and a role
 # that can CREATE DATABASE (CI), each package runs on its own database and -p 1
 # is no longer required. CI uses that path; local shared-LXC cannot CREATE
 # DATABASE and relies on serialisation.
 'test':['go','test','-race','-count=1','-p','1','./...'],
 'build':['go','build','-o','bin/workforce','./cmd/workforce'],
}
# Non-Go suites that are part of the same green bar. The provisioning contract tests
# guard a script that will mutate a live identity provider, so they run with `test`
# rather than depending on someone remembering to run them by hand.
extra_suites={'test':[[sys.executable,'scripts/logto_provision_contract_test.py']]}
def run(cmd):
    print('+ '+' '.join(cmd),flush=True)
    return subprocess.call(cmd,cwd=ROOT,env=env)
code=run(commands[a.action])
for cmd in extra_suites.get(a.action,[]):
    if code!=0: break
    code=run(cmd)
raise SystemExit(code)
