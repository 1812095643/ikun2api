#!/usr/bin/env python3
"""Isolated Linux topology smoke; no real secrets, model requests or recipients."""
import json, os, pathlib, socket, subprocess, tempfile, time, uuid
suffix = uuid.uuid4().hex[:8]
monitor, gateway = 'ops-monitor-test-'+suffix, 'ops-gateway-test-'+suffix
runtime, state = 'ops-runtime-'+suffix, 'ops-state-'+suffix
binary = pathlib.Path(os.environ['MONITOR_LINUX_BINARY']).resolve()
def run(*args, check=True):
    r = subprocess.run(args, text=True, capture_output=True)
    if check and r.returncode: raise RuntimeError(r.stderr[-1800:] or r.stdout[-1800:])
    return r
with tempfile.TemporaryDirectory(prefix='ops-topology-') as directory:
    p = pathlib.Path(directory)
    (p/'gateway.cjs').write_text('''const http=require('http');const now=new Date().toISOString();http.createServer((q,r)=>{if(q.headers.authorization!=='Bearer isolated-fixture-not-a-real-secret'){r.writeHead(401).end();return}r.setHeader('Content-Type','application/json');r.end(JSON.stringify({observation_schema_version:1,site_id:'ikun2api',boot_id:'fixture',sequence:1,oldest_sequence:1,dropped:0,events:[{schema_version:1,site_id:'ikun2api',source_id:'gateway',source_event_id:'one',kind:'attempt.failed',request_id:'test',account_id:1,upstream_status:429,source_quality:'fresh',occurred_at:now,observed_at:now}],active:[],source_capabilities:[]}))}).listen(8080,'0.0.0.0');''')
    (p/'probe.cjs').write_text('''const http=require('http'),fs=require('fs'),assert=require('assert');function call(actor){return new Promise((resolve,reject)=>{const q=http.request({socketPath:'/run/monitor/admin.sock',path:'/v1/summary',headers:actor?{'X-Ops-Actor':'admin:1'}:{}},r=>{let s='';r.on('data',b=>s+=b);r.on('end',()=>resolve({status:r.statusCode,body:JSON.parse(s)}))});q.on('error',reject);q.end()})}(async()=>{assert.equal((await call(false)).status,401);let r=await call(true);assert.equal(r.status,200);assert(r.body.data.incidents.some(i=>i.rule_id==='U01'));assert(r.body.data.incidents.some(i=>i.rule_id==='H01'));assert(r.body.data.sources.some(s=>s.source_id==='gateway'&&s.quality==='fresh'));assert.equal(fs.statSync('/run/monitor/admin.sock').mode&511,432);assert.equal(fs.statSync('/run/monitor/ownership.json').mode&511,416);assert(!fs.existsSync('/var/lib/ikun-ops-monitor/state.db'));assert.throws(()=>fs.writeFileSync('/run/monitor/forbidden','x'));console.log(JSON.stringify({status:'passed',incidents:r.body.data.incidents.length,checks:['nonroot UDS group','401 without actor','gateway evidence through published loopback','PG Redis down cold start','shadow default','read-only runtime mount','no shared state or credentials']}))})().catch(e=>{console.error(e.message);process.exit(1)});''')
    try:
        for v in [runtime,state]: run('docker','volume','create',v)
        run('docker','run','--rm','-v',runtime+':/run/monitor','-v',state+':/state','debian:bookworm-slim','sh','-c','chown 10001:10001 /run/monitor /state && chmod 750 /run/monitor && chmod 700 /state')
        with socket.socket() as reserved:
            reserved.bind(('127.0.0.1',0));fixed_port=reserved.getsockname()[1]
        run('docker','run','-d','--name',gateway,'--user','10002:10002','--group-add','10001','-v',runtime+':/run/monitor:ro','-v',directory+':/fixture:ro','-p',f'127.0.0.1:{fixed_port}:8080','node:22-bookworm','node','/fixture/gateway.cjs')
        port=run('docker','port',gateway,'8080').stdout.strip().rsplit(':',1)[1]
        command=['docker','run','-d','--name',monitor,'--network','host','--user','10001:10001','--cap-drop','ALL','--security-opt','no-new-privileges','--memory','256m','--cpus','.25','-v',str(binary)+':/monitor:ro','-v',runtime+':/run/monitor','-v',state+':/state','-e','OPS_OBSERVATION_TOKEN=isolated-fixture-not-a-real-secret','-e','OPS_POSTGRES_DSN=postgres://fixture@127.0.0.1:1/fixture?sslmode=disable','-e','OPS_REDIS_ADDRESS=127.0.0.1:1','debian:bookworm-slim','/monitor','-state','/state/state.db','-socket','/run/monitor/admin.sock','-health','127.0.0.1:19188','-gateway',f'http://127.0.0.1:{port}/internal/ops-monitor/v1/snapshot']
        run(*command)
        last=None
        for _ in range(30):
            last=run('docker','exec',gateway,'node','/fixture/probe.cjs',check=False)
            if last.returncode==0: break
            time.sleep(1)
        if last.returncode: raise RuntimeError(last.stderr+'; '+run('docker','logs',monitor).stdout)
        print(last.stdout.strip())
        # After gateway exit, the independently running monitor and UDS remain usable.
        run('docker','stop',gateway)
        (p/'outage.cjs').write_text("const h=require('http'),a=require('assert');h.get({socketPath:'/run/monitor/admin.sock',path:'/v1/summary',headers:{'X-Ops-Actor':'admin:1'}},r=>{let s='';r.on('data',b=>s+=b);r.on('end',()=>{const d=JSON.parse(s).data;a(d.sources.some(x=>x.source_id==='gateway'&&x.quality==='stale'));a(d.incidents.some(i=>i.rule_id==='M01'));console.log('gateway outage observed; admin remains available')})}).on('error',e=>{console.error(e.message);process.exit(1)})")
        for _ in range(12):
            outage=run('docker','run','--rm','--user','10002:10002','--group-add','10001','-v',runtime+':/run/monitor:ro','-v',directory+':/fixture:ro','node:22-bookworm','node','/fixture/outage.cjs',check=False)
            if outage.returncode==0: break
            time.sleep(1)
        assert outage.returncode==0,outage.stderr
        print(outage.stdout.strip());run('docker','start',gateway)
        for _ in range(15):
            ready=run('docker','exec',gateway,'node','/fixture/probe.cjs',check=False)
            if ready.returncode==0:break
            time.sleep(1)
        assert ready.returncode==0,ready.stderr
        denied=run('docker','run','--rm','--user','10003:10003','--cap-drop','ALL','-v',runtime+':/run/monitor:ro','-v',directory+':/fixture:ro','node:22-bookworm','node','/fixture/probe.cjs',check=False)
        assert denied.returncode and 'EACCES' in denied.stderr, denied.stderr
        run('docker','restart',monitor)
        for _ in range(15):
            last=run('docker','exec',gateway,'node','/fixture/probe.cjs',check=False)
            if last.returncode==0:break
            time.sleep(1)
        assert last.returncode==0,last.stderr
        stats=run('docker','stats','--no-stream','--format','{{.MemUsage}} | {{.CPUPerc}}',monitor).stdout.strip()
        print(json.dumps({'restart':'passed','wrong_group':'denied','resources':stats}))
    finally:
        for c in [monitor,gateway]: run('docker','rm','-f',c,check=False)
        for v in [runtime,state]: run('docker','volume','rm',v,check=False)
