#!/usr/bin/env python3
"""Run local app roles with external secret files; never print credentials."""
import argparse
import os
from pathlib import Path
import subprocess

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
commands={
 'migrate':['go','run','./cmd/workforce','-migrate'],
 'seed':['go','run','./cmd/workforce','-seed'],
 'api':['go','run','./cmd/workforce'],
 'worker':['go','run','./cmd/workforce','-worker'],
 # -p 1: packages run serially. The api tests start real River workers on the
 # shared database and queue, so parallel packages would steal each other's jobs.
 'test':['go','test','-race','-count=1','-p','1','./...'],
 'build':['go','build','-o','bin/workforce','./cmd/workforce'],
}
raise SystemExit(subprocess.call(commands[a.action],cwd=ROOT,env=env))
