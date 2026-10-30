#!/usr/bin/env python3
"""Switch an existing Sub2API deployment to IKUN without replacing its data.
Requires root, Linux, Python 3.9+, openssl and the existing runtime supervisor.
"""
import argparse,base64,datetime,hashlib,json,os,pathlib,platform,pwd,re,shutil,subprocess,sys,tarfile,tempfile,time,urllib.request

REPO='1812095643/ikun2api'
PUBLIC_KEY='FjAf8K7CSX0LstSKwFGX2sL03GcQ14tUXsbNAAsrGnc='
class SwitchError(RuntimeError):pass

def run(args,*,env=None,cwd=None,user=None,timeout=1800,check=True):
    kwargs={}
    if user is not None:
        record=pwd.getpwnam(user)
        kwargs.update(user=record.pw_uid,group=record.pw_gid,extra_groups=os.getgrouplist(user,record.pw_gid))
    result=subprocess.run(args,capture_output=True,env=env,cwd=cwd,timeout=timeout,**kwargs)
    if check and result.returncode:
        # Config, docker inspect and command environments can contain credentials.
        raise SwitchError('命令失败：'+args[0]+'（详细输出已抑制，避免泄露凭据）')
    return result

def output(args,**kwargs):return run(args,**kwargs).stdout.decode().strip()
def private_write(path,value):
    path=pathlib.Path(path);path.parent.mkdir(parents=True,exist_ok=True);path.write_text(value,encoding='utf8');path.chmod(0o600)
def get(url,limit):
    req=urllib.request.Request(url,headers={'User-Agent':'IKUN2api-Switch','Accept':'application/vnd.github+json'})
    with urllib.request.urlopen(req,timeout=90) as response:
        if response.geturl().split(':',1)[0]!='https':raise SwitchError('拒绝不安全下载')
        value=response.read(limit+1)
    if len(value)>limit:raise SwitchError('下载超过大小限制')
    return value

def verify_checksums(folder):
    folder=pathlib.Path(folder);manifest=(folder/'checksums.txt').read_bytes()
    sig=base64.b64decode((folder/'checksums.txt.sig').read_bytes(),validate=True)
    der=bytes.fromhex('302a300506032b6570032100')+base64.b64decode(PUBLIC_KEY)
    with tempfile.TemporaryDirectory(prefix='ikun-signature-') as temp:
        temp=pathlib.Path(temp);(temp/'key.der').write_bytes(der);(temp/'signature').write_bytes(sig)
        run(['openssl','pkeyutl','-verify','-pubin','-keyform','DER','-inkey',str(temp/'key.der'),'-rawin','-in',str(folder/'checksums.txt'),'-sigfile',str(temp/'signature')])
    entries={}
    for line in manifest.decode('utf8').splitlines():
        parts=line.split()
        if len(parts)!=2 or not re.fullmatch('[0-9a-f]{64}',parts[0]) or '/' in parts[1] or '\\' in parts[1] or parts[1] in entries:raise SwitchError('无效校验清单')
        entries[parts[1]]=parts[0]
    return entries

def prepare_bundle(args,stage):
    machine={'x86_64':'amd64','aarch64':'arm64'}.get(platform.machine())
    if platform.system()!='Linux' or not machine:raise SwitchError('一键迁入支持 Linux amd64/arm64')
    version=args.version
    if not version:
        release=json.loads(get('https://api.github.com/repos/'+REPO+'/releases/latest',2*1024*1024))
        if release.get('draft') or release.get('prerelease'):raise SwitchError('需要正式发布版本')
        version=release['tag_name']
    if not re.fullmatch(r'v?\d+\.\d+\.\d+',version):raise SwitchError('版本号格式无效')
    version=version.removeprefix('v');name=f'ikun2api_{version}_linux_{machine}.tar.gz'
    base=f'https://github.com/{REPO}/releases/download/v{version}/'
    for file,limit in [('checksums.txt',1024*1024),('checksums.txt.sig',1024),(name,500*1024*1024)]:
        value=(args.bundle_dir/file).read_bytes() if args.bundle_dir else get(base+file,limit)
        (stage/file).write_bytes(value)
    entries=verify_checksums(stage)
    if entries.get(name)!=hashlib.sha256((stage/name).read_bytes()).hexdigest():raise SwitchError('安装包校验失败')
    with tarfile.open(stage/name,'r:gz') as archive:
        candidates=[m for m in archive.getmembers() if m.name in ('ikun2api','./ikun2api') and m.isfile()]
        if len(candidates)!=1 or candidates[0].size>500*1024*1024:raise SwitchError('安装包程序无效')
        (stage/'ikun2api').write_bytes(archive.extractfile(candidates[0]).read())
    (stage/'ikun2api').chmod(0o755)
    check=run([str(stage/'ikun2api'),'--version'])
    if 'IKUN2api '+version not in (check.stdout+check.stderr).decode():
        raise SwitchError('安装包版本不一致')
    return version

def parse_info(result):
    lines=result.stdout.decode().splitlines()
    for line in reversed(lines):
        try:value=json.loads(line)
        except ValueError:continue
        if isinstance(value,dict) and 'digests' in value:return value
    raise SwitchError('未取得数据校验结果')

def same_data(before,after):
    if before['counts']!=after['counts'] or before['digests']!=after['digests']:
        raise SwitchError('迁移前后业务数据不一致，切换已停止')

def health(host,port):
    if host in ('0.0.0.0','::',''):host='127.0.0.1'
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for _ in range(90):
        try:
            with opener.open(f'http://{host}:{port}/health',timeout=2) as resp:
                if resp.status==200 and json.load(resp).get('status')=='ok':return
        except Exception:pass
        time.sleep(1)
    raise SwitchError('服务健康检查未通过')

def native_switch(args,stage,version):
    service=args.service or 'sub2api'
    if not re.fullmatch('[A-Za-z0-9_.@-]+',service):raise SwitchError('服务名无效')
    show=lambda name:output(['systemctl','show',service,'--property='+name,'--value'])
    pid=show('MainPID')
    if not pid.isdigit() or int(pid)<=1:raise SwitchError('请先启动原服务，再迁入')
    proc=pathlib.Path('/proc')/pid
    env=dict(x.split('=',1) for x in (proc/'environ').read_bytes().decode().split('\0') if '=' in x)
    cwd=os.readlink(proc/'cwd');original_exe=os.readlink(proc/'exe');user=show('User') or 'root'
    argv=[x.decode() for x in (proc/'cmdline').read_bytes().split(b'\0') if x]
    if len(argv)!=1:raise SwitchError('原服务有自定义启动参数，请保留参数后手动迁入')
    if pathlib.Path(argv[0]).name not in ('sub2api','sub6api','ikun2api'):raise SwitchError('该服务不是 Sub2API/IKUN2api')
    root=args.install_dir.resolve();runtime=root/'runtime';runtime.mkdir(parents=True,exist_ok=True)
    uid=pwd.getpwnam(user);os.chown(runtime,uid.pw_uid,uid.pw_gid)
    binary=runtime/'ikun2api';candidate=runtime/'.ikun2api.switch'
    shutil.copy2(stage/'ikun2api',candidate);candidate.chmod(0o755)
    run([str(candidate),'--upgrade-preflight'],env=env,cwd=cwd,user=user,timeout=90)
    before=parse_info(run([str(candidate),'--upgrade-info'],env=env,cwd=cwd,user=user,timeout=90))
    if args.check:print(json.dumps({'compatible':True,'mode':'systemd','version':version,'counts':before['counts']},ensure_ascii=False));return
    stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    backup=args.backup_dir.resolve()/stamp;backup.mkdir(mode=0o700,parents=True)
    private_write(backup/'original-environment.json',json.dumps(env))
    private_write(backup/'original-unit.txt',output(['systemctl','cat',service]))
    private_write(backup/'before.json',json.dumps(before))
    dropin=pathlib.Path('/etc/systemd/system')/(service+'.service.d')/'90-ikun2api.conf'
    if dropin.exists():raise SwitchError('已存在 IKUN 切换配置，请在页面里更新')
    marker=runtime/'.migration-maintenance';marker.write_text('migration');marker.chmod(0o644)
    stopped=False;changed=False;snapshot=False
    try:
        shutil.copy2(original_exe,backup/'previous-binary')
        run(['systemctl','stop',service]);stopped=True
        before=parse_info(run([str(candidate),'--upgrade-info'],env=env,cwd=cwd,user=user,timeout=90))
        run([str(candidate),'--upgrade-backup-dir',str(backup/'snapshot')],env=env,cwd=cwd);snapshot=True
        os.replace(candidate,binary);os.chown(binary,uid.pw_uid,uid.pw_gid)
        dropin.parent.mkdir(parents=True,exist_ok=True)
        private_write(dropin,'[Service]\nExecStart=\nExecStart='+str(binary)+' --supervise\nRestart=always\nReadWritePaths='+str(runtime)+'\nEnvironment=IKUN_MAINTENANCE_FILE='+str(marker)+'\n')
        changed=True;run(['systemctl','daemon-reload']);run(['systemctl','start',service]);stopped=False
        health(before['server_host'],before['server_port'])
        after=parse_info(run([str(binary),'--upgrade-info'],env=env,cwd=cwd,user=user,timeout=90));same_data(before,after)
        marker.unlink();private_write(backup/'SUCCESS.json',json.dumps({'version':version,'counts':after['counts'],'service':service}))
        print(json.dumps({'success':True,'mode':'systemd','version':version,'counts':after['counts'],'backup':str(backup)},ensure_ascii=False))
    except Exception:
        if changed:
            run(['systemctl','stop',service],check=False)
            if snapshot:run([str(binary),'--upgrade-restore-file',str(backup/'snapshot/database.sql')],env=env,cwd=cwd)
            dropin.unlink(missing_ok=True);run(['systemctl','daemon-reload'])
        marker.unlink(missing_ok=True)
        if stopped or changed:run(['systemctl','start',service])
        raise

def compose_switch(args,stage,version):
    files=[str(p.resolve()) for p in args.compose_file]
    if not files:
        files=[str(p.resolve()) for p in [pathlib.Path('docker-compose.yml'),pathlib.Path('docker-compose.local.yml'),pathlib.Path('compose.yml')] if p.exists()][:1]
    if not files:raise SwitchError('未找到 Compose 文件，请使用 --compose-file 指定原文件')
    command=['docker','compose']
    for file in files:command+=['-f',file]
    service=args.compose_service or 'sub2api'
    cid=output(command+['ps','-q',service])
    if not cid or '\n' in cid:raise SwitchError('需要一个正在运行的原服务容器')
    config=json.loads(output(['docker','inspect',cid]))[0]
    if not config['State']['Running']:raise SwitchError('请先启动原服务')
    networks=list(config['NetworkSettings']['Networks'])
    if len(networks)!=1:raise SwitchError('多网络部署需要确认数据库网络后手动迁入')
    if not any(m['Destination']=='/app/data' and m.get('RW') for m in config['Mounts']):raise SwitchError('原服务必须有可写持久化 /app/data')
    image=f'ghcr.io/{REPO}:{version}'
    pulled=run(['docker','pull',image],check=False)
    if pulled.returncode:
        name=f'ikun2api_image_{version}_linux_'+{'x86_64':'amd64','aarch64':'arm64'}[platform.machine()]+'.tar.gz'
        value=(args.bundle_dir/name).read_bytes() if args.bundle_dir else get(f'https://github.com/{REPO}/releases/download/v{version}/'+name,1024*1024*1024)
        if verify_checksums(stage).get(name)!=hashlib.sha256(value).hexdigest():raise SwitchError('容器镜像校验失败')
        archive=stage/name;archive.write_bytes(value);run(['docker','load','--input',str(archive)])
    image_hash=output(['docker','run','--rm','--entrypoint','sha256sum',image,'/app/ikun2api']).split()[0]
    if image_hash!=hashlib.sha256((stage/'ikun2api').read_bytes()).hexdigest():raise SwitchError('容器程序与签名安装包不一致')
    envfile=stage/'docker.environment';private_write(envfile,'\n'.join(config['Config']['Env'])+'\n')
    network=networks[0]
    def helper(arguments,container=cid):return run(['docker','run','--rm','--network',network,'--volumes-from',container,'--env-file',str(envfile),'--entrypoint','/app/ikun2api',image]+arguments)
    helper(['--upgrade-preflight']);before=parse_info(helper(['--upgrade-info']))
    if args.check:print(json.dumps({'compatible':True,'mode':'compose','version':version,'counts':before['counts']},ensure_ascii=False));return
    stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ');backup='/app/data/ikun-switch-backups/'+stamp
    override=pathlib.Path(files[0]).parent/'ikun2api.override.yml'
    if override.exists():raise SwitchError('已存在 IKUN Compose 配置，请在页面里更新')
    marker='/app/data/.ikun-switch-maintenance';current=cid;changed=False;snapshot=False
    try:
        run(command+['stop',service]);before=parse_info(helper(['--upgrade-info']))
        helper(['--upgrade-backup-dir',backup]);snapshot=True
        run(['docker','run','--rm','--volumes-from',cid,'--entrypoint','sh',image,'-c','umask 022; : > /app/data/.ikun-switch-maintenance'])
        value={'services':{service:{'image':image,'entrypoint':['/app/docker-entrypoint.sh'],'command':['/app/ikun2api'],'restart':'unless-stopped','environment':{'IKUN_MAINTENANCE_FILE':marker}}}}
        private_write(override,json.dumps(value,indent=2))
        changed=True;run(command+['-f',str(override),'up','-d','--no-deps','--no-build',service])
        current=output(command+['-f',str(override),'ps','-q',service])
        for _ in range(90):
            if run(['docker','exec',current,'curl','-fsS','http://127.0.0.1:'+str(before['server_port'])+'/health'],check=False,timeout=5).returncode==0:break
            time.sleep(1)
        else:raise SwitchError('容器健康检查未通过')
        after=parse_info(helper(['--upgrade-info'],current));same_data(before,after)
        run(['docker','exec',current,'rm','-f',marker])
        print(json.dumps({'success':True,'mode':'compose','version':version,'counts':after['counts'],'backup':backup,'compose_override':str(override)},ensure_ascii=False))
    except Exception:
        if changed:
            candidate_id=output(command+['-f',str(override),'ps','--all','-q',service])
            if candidate_id:current=candidate_id
            run(command+['-f',str(override),'stop',service],check=False)
        if snapshot:helper(['--upgrade-restore-file',backup+'/database.sql'],current)
        run(['docker','run','--rm','--volumes-from',current,'--entrypoint','sh',image,'-c','rm -f /app/data/.ikun-switch-maintenance'])
        override.unlink(missing_ok=True)
        rollback=stage/'rollback.json';private_write(rollback,json.dumps({'services':{service:{'image':config['Image']}}}))
        run(command+['-f',str(rollback),'up','-d','--no-deps','--no-build',service])
        raise

def main():
    parser=argparse.ArgumentParser(description='保留原数据迁入 IKUN2api；后续从管理页面更新')
    parser.add_argument('--mode',choices=['auto','systemd','compose'],default='auto');parser.add_argument('--service')
    parser.add_argument('--compose-file',type=pathlib.Path,action='append',default=[]);parser.add_argument('--compose-service')
    parser.add_argument('--install-dir',type=pathlib.Path,default=pathlib.Path('/opt/ikun2api'));parser.add_argument('--backup-dir',type=pathlib.Path,default=pathlib.Path('/opt/ikun2api-switch-backups'))
    parser.add_argument('--version');parser.add_argument('--bundle-dir',type=pathlib.Path);parser.add_argument('--check',action='store_true')
    args=parser.parse_args()
    if os.geteuid()!=0:raise SwitchError('请使用 sudo 运行，才能备份原资料并切换服务')
    if not shutil.which('openssl'):raise SwitchError('请先安装 openssl')
    mode=args.mode
    if mode=='auto':mode='systemd' if run(['systemctl','is-active','--quiet',args.service or 'sub2api'],check=False).returncode==0 else 'compose'
    with tempfile.TemporaryDirectory(prefix='ikun-switch-') as temp:
        stage=pathlib.Path(temp);stage.chmod(0o755);version=prepare_bundle(args,stage)
        print('安装包已校验，正在检查原部署数据兼容性。',flush=True)
        (native_switch if mode=='systemd' else compose_switch)(args,stage,version)
if __name__=='__main__':
    try:main()
    except (SwitchError,Exception) as error:
        print('迁入未完成：'+str(error),file=sys.stderr);sys.exit(1)
