#!/usr/bin/env python3
"""Rehearse a complete Compose switch on disposable containers and volumes."""
import hashlib,io,json,os,pathlib,secrets,shutil,subprocess,tarfile,tempfile,time,urllib.request
ROOT=pathlib.Path(__file__).resolve().parents[2]
VERSION=(ROOT/'backend/cmd/server/VERSION').read_text().strip()
def run(args,**kwargs):
    r=subprocess.run(args,capture_output=True,timeout=1200,**kwargs)
    if r.returncode:raise RuntimeError('Command failed: '+args[0]+'\n'+(r.stdout+r.stderr).decode(errors='replace')[-5000:])
    return r.stdout.decode().strip()
def download(url):
    with urllib.request.urlopen(urllib.request.Request(url,headers={'User-Agent':'IKUN-release-QA'}),timeout=180) as r:return r.read()
def main():
    bundle=ROOT/'dist/release';project='ikun-migration-qa-'+secrets.token_hex(4);image='ghcr.io/1812095643/ikun2api:'+VERSION
    run(['docker','load','-i',str(bundle/f'ikun2api_image_{VERSION}_linux_amd64.tar.gz')])
    with tempfile.TemporaryDirectory(prefix='ikun-compose-qa-') as temp:
        work=pathlib.Path(temp);data=work/'data';data.mkdir();(data/'.installed').write_text('fixture')
        sqlroot=work/'official-sql';sqlroot.mkdir()
        source=download('https://api.github.com/repos/Wei-Shaw/sub2api/tarball/v0.2.19')
        with tarfile.open(fileobj=io.BytesIO(source),mode='r:gz') as tar:
            for member in tar:
                if '/backend/migrations/' in member.name and member.name.endswith('.sql') and member.isfile():
                    (sqlroot/pathlib.PurePosixPath(member.name).name).write_bytes(tar.extractfile(member).read())
        archive_name='sub2api_0.2.19_linux_amd64.tar.gz';base='https://github.com/Wei-Shaw/sub2api/releases/download/v0.2.19/'
        original=download(base+archive_name);checksum=download(base+'checksums.txt').decode()
        assert any(line.split()==[hashlib.sha256(original).hexdigest(),archive_name] for line in checksum.splitlines())
        binary=work/'sub2api'
        with tarfile.open(fileobj=io.BytesIO(original),mode='r:gz') as tar:
            member=next(x for x in tar if pathlib.PurePosixPath(x.name).name=='sub2api' and x.isfile());binary.write_bytes(tar.extractfile(member).read())
        binary.chmod(0o755)
        password='fixture-'+secrets.token_hex(16)
        env={'DATABASE_HOST':'postgres','DATABASE_PORT':'5432','DATABASE_USER':'fixture','DATABASE_PASSWORD':password,'DATABASE_DBNAME':'fixture','DATABASE_SSLMODE':'disable','REDIS_HOST':'redis','REDIS_PORT':'6379','REDIS_DB':'0','DATA_DIR':'/app/data','SERVER_HOST':'0.0.0.0','SERVER_PORT':'8080','JWT_SECRET':secrets.token_hex(32),'TOTP_ENCRYPTION_KEY':secrets.token_hex(32),'SUB6_CLAUDE_TELEMETRY_ENABLED':'false','LOG_FILE_ENABLED':'false'}
        compose={'services':{
          'postgres':{'image':'postgres:18-alpine','environment':{'POSTGRES_DB':'fixture','POSTGRES_USER':'fixture','POSTGRES_PASSWORD':password},'ports':['127.0.0.1::5432'],'volumes':['postgres_data:/var/lib/postgresql']},
          'redis':{'image':'redis:7-alpine','command':['redis-server','--save','','--appendonly','no']},
          'sub2api':{'image':image,'entrypoint':['/opt/original-sub2api'],'command':[],'environment':env,'volumes':[str(data)+':/app/data',str(binary)+':/opt/original-sub2api:ro'],'ports':['127.0.0.1::8080'],'depends_on':['postgres','redis'],'restart':'unless-stopped'}},'volumes':{'postgres_data':{}}}
        file=work/'compose.json';file.write_text(json.dumps(compose));command=['docker','compose','-p',project,'-f',str(file)]
        try:
            run(command+['up','-d','postgres','redis'])
            postgres=run(command+['ps','-q','postgres'])
            for _ in range(60):
                if subprocess.run(['docker','exec',postgres,'pg_isready','-U','fixture','-d','fixture'],capture_output=True).returncode==0:break
                time.sleep(1)
            dbport=json.loads(run(['docker','inspect',postgres]))[0]['NetworkSettings']['Ports']['5432/tcp'][0]['HostPort']
            fixtureenv={**os.environ,'IKUN_UPGRADE_TEST_DSN':f'host=127.0.0.1 port={dbport} user=fixture password={password} dbname=fixture sslmode=disable','IKUN_UPGRADE_TEST_MIGRATIONS':str(sqlroot),'IKUN_UPGRADE_TEST_BASELINE_ONLY':'1'}
            result=run(['go','test','-tags','unit','./internal/repository','-run','^TestUpgradeOfficialSnapshotsPreserveData$','-count=1','-v'],cwd=ROOT/'backend',env=fixtureenv);assert 'OFFICIAL_BASELINE_READY' in result
            # Snapshot the exact original schema/data before it is opened by either server.
            def sql(q):return run(['docker','exec',postgres,'psql','-X','-U','fixture','-d','fixture','-Atqc',q])
            def fingerprint():return {name:sql(query) for name,query in {
             'users':"SELECT md5(json_agg(json_build_array(id,email,password_hash,balance,role,status) ORDER BY id)::text) FROM users",
             'accounts':"SELECT md5(json_agg(json_build_array(id,platform,type,credentials,status) ORDER BY id)::text) FROM accounts",
             'keys':"SELECT md5(json_agg(json_build_array(id,user_id,key,group_id,status) ORDER BY id)::text) FROM api_keys",
             'subscriptions':"SELECT md5(json_agg(json_build_array(id,user_id,group_id,starts_at,expires_at,status,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) ORDER BY id)::text) FROM user_subscriptions",
             'usage':'SELECT count(*) FROM usage_logs',
             'redeem':'SELECT count(*) FROM redeem_codes'}.items()}
            before=fingerprint();run(command+['up','-d','sub2api'])
            container=run(command+['ps','-q','sub2api']);hostport=json.loads(run(['docker','inspect',container]))[0]['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort'];url='http://127.0.0.1:'+hostport
            opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
            def api(path,payload=None,token=None):
                headers={'Content-Type':'application/json','X-Admin-UI-Request':'1'}
                if token:headers['Authorization']='Bearer '+token
                req=urllib.request.Request(url+path,headers=headers,data=json.dumps(payload).encode() if payload is not None else None)
                with opener.open(req,timeout=10) as response:return json.load(response).get('data',{})
            def ready():
                for _ in range(120):
                    try:
                        with opener.open(url+'/health',timeout=2) as response:
                            if response.status==200:return
                    except Exception:pass
                    time.sleep(1)
                raise RuntimeError('health timed out')
            ready();token=api('/api/v1/auth/login',{'email':'migration-fixture@example.invalid','password':'MigrationFixtureOnly!23'})['access_token'];assert token and fingerprint()==before
            # Preserve the exact project name and original files when creating the override.
            migrationenv={**os.environ,'COMPOSE_PROJECT_NAME':project}
            result=run(['python3',str(ROOT/'deploy/switch-from-sub2api.py'),'--mode','compose','--compose-file',str(file),'--compose-service','sub2api','--bundle-dir',str(bundle),'--version',VERSION],env=migrationenv)
            report=json.loads(result.splitlines()[-1]);assert report['success']
            newcommand=command+['-f',str(work/'ikun2api.override.yml')];container=run(newcommand+['ps','-q','sub2api']);hostport=json.loads(run(['docker','inspect',container]))[0]['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort'];url='http://127.0.0.1:'+hostport
            ready();token=api('/api/v1/auth/login',{'email':'migration-fixture@example.invalid','password':'MigrationFixtureOnly!23'})['access_token'];ack=api('/api/v1/admin/compliance',token=token);api('/api/v1/admin/compliance/accept',{'phrase':ack['ack_phrase_zh'],'language':'zh'},token)
            assert api('/api/v1/admin/system/version',token=token)['version']==VERSION;assert fingerprint()==before
            # The updated runtime survives both a restart and a same-image recreate.
            for action in [['restart','sub2api'],['up','-d','--no-deps','--force-recreate','sub2api']]:
                run(newcommand+action);container=run(newcommand+['ps','-q','sub2api']);hostport=json.loads(run(['docker','inspect',container]))[0]['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort'];url='http://127.0.0.1:'+hostport;ready();assert fingerprint()==before
            report.update(isolated=True,official_binary='0.2.19',login_preserved=True,restart_and_recreate_preserve_data=True,provider_model_requests_sent=0)
            (bundle/'compose-rehearsal.json').write_text(json.dumps(report,indent=2));print(json.dumps(report))
        finally:
            # Only disposable containers/volumes belonging to the random QA project.
            subprocess.run(command+['-f',str(work/'ikun2api.override.yml'),'down','-v','--remove-orphans'] if (work/'ikun2api.override.yml').exists() else command+['down','-v','--remove-orphans'],capture_output=True)
if __name__=='__main__':main()
