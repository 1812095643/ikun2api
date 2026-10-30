#!/usr/bin/env python3
"""Build one release target from the verified source checkout."""
import argparse,datetime,json,os,pathlib,re,shutil,subprocess,tarfile,tempfile,zipfile
ROOT=pathlib.Path(__file__).resolve().parents[2]
def build(target,version,commit,output):
 if not re.fullmatch(r'\d+\.\d+\.\d+',version):raise ValueError('stable semantic version required')
 if target not in ('linux_amd64','linux_arm64','darwin_amd64','darwin_arm64','windows_amd64'):raise ValueError('unsupported target')
 system,arch=target.split('_');output.mkdir(exist_ok=True,parents=True)
 with tempfile.TemporaryDirectory(prefix='ikun-release-') as temp:
  stage=pathlib.Path(temp);binary=stage/('ikun2api.exe' if system=='windows' else 'ikun2api')
  env={**os.environ,'CGO_ENABLED':'0','GOOS':system,'GOARCH':arch}
  date=os.environ.get('BUILD_DATE') or datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
  subprocess.run([os.environ.get('GO_BINARY','go'),'build','-trimpath','-tags=embed','-ldflags',f'-s -w -X main.Version={version} -X main.Commit={commit} -X main.Date={date} -X main.BuildType=release','-o',str(binary),'./cmd/server'],cwd=ROOT/'backend',env=env,check=True)
  shutil.copytree(ROOT/'deploy',stage/'deploy',ignore=shutil.ignore_patterns('__pycache__','*.pyc','config.yaml','.env'))
  shutil.copytree(ROOT/'backend/resources',stage/'resources')
  for file in ['LICENSE','COPYING.GPL','NOTICE.md','README.md']:
   if (ROOT/file).exists():shutil.copy2(ROOT/file,stage/file)
  (stage/'build-info.json').write_text(json.dumps({'version':version,'commit':commit,'target':target,'built_at':date},indent=2))
  name=f'ikun2api_{version}_{target}'
  if system=='windows':
   dest=output/(name+'.zip')
   with zipfile.ZipFile(dest,'w',zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(stage.rglob('*')):
     if path.is_file():archive.write(path,path.relative_to(stage))
  else:
   dest=output/(name+'.tar.gz')
   with tarfile.open(dest,'w:gz') as archive:
    for path in sorted(stage.iterdir()):archive.add(path,arcname=path.name)
  print(json.dumps({'archive':dest.name,'version':version,'commit':commit,'target':target}))
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--target',required=True);p.add_argument('--version',required=True);p.add_argument('--commit',required=True);p.add_argument('--output',type=pathlib.Path,default=ROOT/'dist/release');a=p.parse_args();build(a.target,a.version,a.commit,a.output.resolve())
