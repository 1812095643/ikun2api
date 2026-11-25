#!/usr/bin/env python3
"""Export only approved monitor SELECT projections via an existing SSH login.
No views/roles are installed. Raw prompts, URLs, error bodies and secrets are
excluded by the checked-in SELECTs. Output still contains internal IDs: private.
"""
import argparse,collections,datetime,hashlib,json,os,pathlib,re,shlex,subprocess
p=argparse.ArgumentParser();p.add_argument('--host',required=True);p.add_argument('--known-hosts',required=True);p.add_argument('--control-path');p.add_argument('--container',default='ikun2api-postgres');p.add_argument('--until',required=True,help='Fixed ISO timestamp with timezone');p.add_argument('--output',required=True);a=p.parse_args()
end=datetime.datetime.fromisoformat(a.until.replace('Z','+00:00'))
if end.tzinfo is None:raise SystemExit('--until must include timezone')
start=end-datetime.timedelta(hours=24)
root=pathlib.Path(__file__).resolve().parents[3]
sql=(root/'backend/migrations/274_ops_monitor_readonly_views.sql').read_text()
queries=[]
for view in ['errors','attempts','usage','system']:
 match=re.search(r'CREATE OR REPLACE VIEW ops_monitor_v1_'+view+r' AS\n(.*?);',sql,re.S)
 if not match:raise SystemExit('Missing approved SELECT: '+view)
 queries.append("WITH evidence_view AS ("+match.group(1)+") SELECT json_build_object('source','"+view+"','id',id::text,'at',observed_at,'evidence',evidence) FROM evidence_view WHERE observed_at>='"+start.isoformat()+"' AND observed_at<'"+end.isoformat()+"' ORDER BY observed_at,id;")
args=['ssh','-o','BatchMode=yes','-o','ConnectTimeout=10','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+a.known_hosts]
if a.control_path:args+=['-S',a.control_path]
remote='docker exec -i '+shlex.quote(a.container)+' sh -c '+shlex.quote('exec psql -XqAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"')
args += [a.host,remote]
result=subprocess.run(args,input="BEGIN READ ONLY; SET LOCAL statement_timeout='15000ms';\n"+'\n'.join(queries)+'\nROLLBACK;',text=True,capture_output=True,timeout=90)
if result.returncode:raise SystemExit('Read-only export failed; no output created. Check SSH and database schema.')
rows=[json.loads(line) for line in result.stdout.splitlines()]
out=pathlib.Path(a.output);out.parent.mkdir(parents=True,exist_ok=True)
fd=os.open(out,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f:f.write(result.stdout)
print(json.dumps({'window_start':start.isoformat(),'window_end':end.isoformat(),'counts':dict(collections.Counter(r['source'] for r in rows)),'sha256':hashlib.sha256(result.stdout.encode()).hexdigest(),'production_writes':False},ensure_ascii=False))
