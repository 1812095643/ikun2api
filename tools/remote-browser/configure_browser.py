import json
import os
import pwd
import re
import sys
import secrets
import shutil
import string
import subprocess
from pathlib import Path

root = Path('/opt/ikun2api-remote-browser')
state = Path('/var/lib/ikun2api-browser')

def run(args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs)

def write(path, content, mode=0o644, owner=None):
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding='utf-8')
    target.chmod(mode)
    if owner:
        shutil.chown(target, user=owner, group=owner)

for user, home in [('ikun2apibrowser', str(state)), ('ikun2apibrowserproxy', '/nonexistent'), ('ikun2apinovnc', '/nonexistent')]:
    try:
        pwd.getpwnam(user)
    except KeyError:
        run(['useradd', '--system', '--user-group', '--home-dir', home, '--shell', '/usr/sbin/nologin', user])

state.mkdir(mode=0o700, parents=True, exist_ok=True)
state.chmod(0o700)
shutil.chown(state, user='ikun2apibrowser', group='ikun2apibrowser')
downloads = state / 'Downloads'
downloads.mkdir(mode=0o700, exist_ok=True)
shutil.chown(downloads, user='ikun2apibrowser', group='ikun2apibrowser')

settings_path = Path(sys.argv[1]).resolve()
if settings_path.stat().st_mode & 0o077:
    raise RuntimeError('Settings file permissions must be 0600')
settings = json.loads(settings_path.read_text())
server_name = settings['server_name']
if not re.fullmatch(r'[A-Za-z0-9.-]+', server_name):
    raise RuntimeError('Invalid server name')
for field in ['tls_certificate', 'tls_certificate_key']:
    if not re.fullmatch(r'/[A-Za-z0-9_./-]+', settings[field]) or not Path(settings[field]).is_file():
        raise RuntimeError('Expected an existing absolute TLS certificate path')
browser_origin = 'https://' + server_name + ':9443'
proxy = settings['upstream_socks5']
if not isinstance(proxy.get('host'), str) or not proxy['host'] or not 1 <= int(proxy['port']) <= 65535:
    raise RuntimeError('Expected an upstream SOCKS5 endpoint')
proxy['port'] = int(proxy['port'])
xray = {
    'log': {'loglevel': 'warning'},
    'inbounds': [{'tag':'browser-http','listen':'127.0.0.1','port':18080,'protocol':'http','settings':{'allowTransparent':False}}],
    'outbounds': [{'tag':'singapore','protocol':'socks','settings':{'address':proxy['host'],'port':proxy['port'],'user':proxy['username'],'pass':proxy['password']}}],
    'routing': {'domainStrategy':'AsIs','rules':[{'type':'field','inboundTag':['browser-http'],'outboundTag':'singapore'}]}
}
write(root / 'proxy.json', json.dumps(xray, indent=2)+'\n', 0o600, 'ikun2apibrowserproxy')
run(['/usr/local/bin/xray','run','-test','-config',str(root / 'proxy.json')])
proxy = None

credentials_path = root / 'access.json'
if credentials_path.exists():
    credentials = json.loads(credentials_path.read_text())
else:
    credentials = {'url':browser_origin+'/vnc.html?autoconnect=1&resize=remote&path=websockify','username':'browser','password':secrets.token_urlsafe(24),'vnc_password':''.join(secrets.choice(string.ascii_letters+string.digits) for _ in range(8))}
    write(credentials_path, json.dumps(credentials, indent=2)+'\n', 0o600)
password_hash = run(['openssl','passwd','-6','-stdin'], input=credentials['password']+'\n').stdout.strip()
write(root / 'htpasswd', credentials['username']+':'+password_hash+'\n', 0o640)
shutil.chown(root / 'htpasswd', user='root', group='www-data')
vnc = subprocess.run(['/usr/bin/tigervncpasswd','-f'], input=(credentials['vnc_password']+'\n').encode(), capture_output=True, check=True)
(state / 'vnc.pass').write_bytes(vnc.stdout)
(state / 'vnc.pass').chmod(0o600)
shutil.chown(state / 'vnc.pass', user='ikun2apibrowser', group='ikun2apibrowser')

policy = {'policies':{
    'Proxy':{'Mode':'manual','Locked':True,'HTTPProxy':'127.0.0.1:18080','SSLProxy':'127.0.0.1:18080','UseHTTPProxyForAllProtocols':True,'AutoLogin':False,'Passthrough':''},
    'DNSOverHTTPS':{'Enabled':False,'Locked':True,'Fallback':False}
}}
policy_path = Path('/etc/firefox/policies/policies.json')
if policy_path.exists() and json.loads(policy_path.read_text()) != policy:
    raise RuntimeError('An unrelated Firefox policy already exists')
write(policy_path, json.dumps(policy, indent=2)+'\n')
prefs = {
    'intl.accept_languages':'zh-SG,en-SG,en',
    'intl.locale.requested':'zh-SG,en-SG,en',
    'media.peerconnection.ice.proxy_only':True,
    'geo.enabled':False,
    'permissions.default.geo':2,
    'browser.startup.page':0,
    'browser.startup.homepage':'about:blank',
    'browser.sessionstore.resume_from_crash':False,
    'browser.sessionstore.resume_session_once':False,
    'browser.privatebrowsing.autostart':False,
    'privacy.sanitize.sanitizeOnShutdown':False,
    'network.dns.disablePrefetch':True,
    'network.prefetch-next':False,
    'network.predictor.enabled':False,
    'network.proxy.failover_direct':False,
    'network.proxy.no_proxies_on':'',
    'browser.cache.disk.capacity':102400,
    'dom.ipc.processCount':2,
    'dom.ipc.processCount.webIsolated':1
}
policy['policies']['Preferences'] = {key:{'Value':value,'Status':'locked'} for key,value in prefs.items()}
write(policy_path, json.dumps(policy, indent=2)+'\n')
write(root / 'browser-preferences.json', json.dumps(prefs, indent=2)+'\n', 0o644)
write(root / 'ephemeral_browser.py', Path(__file__).with_name('ephemeral_browser.py').read_text(encoding='utf-8'), 0o755)
nss = Path('/etc/nsswitch.conf').read_text().splitlines()
write(root / 'nsswitch.conf', '\n'.join('hosts: files dns' if line.startswith('hosts:') else line for line in nss)+'\n')
write(root / 'resolv.conf', 'nameserver 127.0.0.254\noptions timeout:1 attempts:1\n')

uid = pwd.getpwnam('ikun2apibrowser').pw_uid
firewall = '''set -eu
for binary in /usr/sbin/iptables /usr/sbin/ip6tables; do
    "$binary" -w -N SUB6_BROWSER 2>/dev/null || true
    "$binary" -w -F SUB6_BROWSER
    "$binary" -w -A SUB6_BROWSER -o lo -p tcp -d 127.0.0.1 --dport 18080 -j ACCEPT 2>/dev/null || true
    "$binary" -w -A SUB6_BROWSER -o lo -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
    "$binary" -w -A SUB6_BROWSER -j REJECT
    "$binary" -w -C OUTPUT -m owner --uid-owner BROWSER_UID -j SUB6_BROWSER 2>/dev/null || "$binary" -w -I OUTPUT 1 -m owner --uid-owner BROWSER_UID -j SUB6_BROWSER
done
'''.replace('BROWSER_UID', str(uid))
write(root / 'firewall.sh', firewall, 0o700)
desktop = '''set -eu
exec /usr/bin/python3 /opt/ikun2api-remote-browser/ephemeral_browser.py --runtime-dir /run/ikun2api-browser --preferences /opt/ikun2api-remote-browser/browser-preferences.json --downloads-dir /var/lib/ikun2api-browser/Downloads
'''
write(root / 'desktop.sh', desktop, 0o755)
session = '''set -eu
export DISPLAY=unix/:2
export XAUTHORITY=/run/ikun2api-browser/Xauthority
touch "$XAUTHORITY"
chmod 600 "$XAUTHORITY"
/usr/bin/xauth -f "$XAUTHORITY" add :2 . "$(/usr/bin/mcookie)"
/usr/bin/Xtigervnc :2 -auth "$XAUTHORITY" -localhost yes -rfbport 5902 -SecurityTypes VncAuth -PasswordFile /var/lib/ikun2api-browser/vnc.pass -geometry 1366x900 -AcceptSetDesktopSize=0 -depth 24 -FrameRate 24 -nolisten tcp &
vnc_pid=$!
desktop_pid=
trap 'test -z "$desktop_pid" || kill "$desktop_pid" 2>/dev/null || true; kill "$vnc_pid" 2>/dev/null || true' EXIT TERM INT
count=0
while ! /usr/bin/timeout 2s /usr/bin/xdpyinfo >/dev/null 2>&1; do
    count=$((count + 1))
    test "$count" -lt 100 || exit 1
    kill -0 "$vnc_pid"
    sleep 0.1
done
/usr/bin/dbus-run-session -- /bin/sh /opt/ikun2api-remote-browser/desktop.sh &
desktop_pid=$!
wait "$desktop_pid"
'''
write(root / 'session.sh', session, 0o755)

write('/etc/systemd/system/ikun2api-browser-firewall.service', '''[Unit]
Description=IKUN2api browser user egress isolation
After=ufw.service
Before=ikun2api-browser.service

[Service]
Type=oneshot
ExecStart=/bin/sh /opt/ikun2api-remote-browser/firewall.sh
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
''')
write('/etc/systemd/system/ikun2api-browser-proxy.service', '''[Unit]
Description=IKUN2api browser Singapore proxy
After=network-online.target
Wants=network-online.target

[Service]
User=ikun2apibrowserproxy
Group=ikun2apibrowserproxy
ExecStart=/usr/local/bin/xray run -config /opt/ikun2api-remote-browser/proxy.json
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
MemoryHigh=64M
MemoryMax=96M
LimitNOFILE=16384

[Install]
WantedBy=multi-user.target
''')
write('/etc/systemd/system/ikun2api-browser.service', '''[Unit]
Description=IKUN2api remote Firefox session
After=network-online.target ikun2api-browser-proxy.service ikun2api-browser-firewall.service
Requires=ikun2api-browser-proxy.service ikun2api-browser-firewall.service
StartLimitIntervalSec=300
StartLimitBurst=3

[Service]
User=ikun2apibrowser
Group=ikun2apibrowser
WorkingDirectory=/var/lib/ikun2api-browser
RuntimeDirectory=ikun2api-browser
RuntimeDirectoryMode=0700
Environment=HOME=/var/lib/ikun2api-browser
Environment=XDG_RUNTIME_DIR=/run/ikun2api-browser
Environment=TZ=Asia/Singapore
Environment=LANG=zh_SG.UTF-8
Environment=LC_ALL=zh_SG.UTF-8
Environment=LANGUAGE=zh_SG:en_SG:en
ExecStart=/bin/sh /opt/ikun2api-remote-browser/session.sh
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/ikun2api-browser /run/ikun2api-browser
BindReadOnlyPaths=/opt/ikun2api-remote-browser/nsswitch.conf:/etc/nsswitch.conf /opt/ikun2api-remote-browser/resolv.conf:/etc/resolv.conf
MemoryHigh=480M
MemoryMax=640M
MemorySwapMax=768M
CPUQuota=80%
OOMScoreAdjust=350
TimeoutStopSec=20

[Install]
WantedBy=multi-user.target
''')
write('/etc/systemd/system/ikun2api-browser-web.service', '''[Unit]
Description=IKUN2api remote browser WebSocket transport
After=ikun2api-browser.service
Wants=ikun2api-browser.service

[Service]
User=ikun2apinovnc
Group=ikun2apinovnc
ExecStart=/usr/bin/websockify --heartbeat=30 127.0.0.1:6082 127.0.0.1:5902
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
MemoryHigh=64M
MemoryMax=96M

[Install]
WantedBy=multi-user.target
''')
write('/etc/nginx/conf.d/ikun-browser-logging.conf', '''log_format ikun_browser_access escape=json '{"time":"$time_iso8601","remote_addr":"$remote_addr","server_port":$server_port,"method":"$request_method","uri":"$uri","status":$status,"bytes":$body_bytes_sent,"request_time":$request_time,"upstream_status":"$upstream_status"}';
''')
write('/etc/nginx/sites-available/ikun2api-browser', '''server {
    listen 9443 ssl;
    server_name IKUN_BROWSER_HOST;
    ssl_certificate IKUN_BROWSER_CERTIFICATE;
    ssl_certificate_key IKUN_BROWSER_PRIVATE_KEY;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:SUB6_BROWSER:1m;
    auth_basic "Remote browser";
    auth_basic_user_file /opt/ikun2api-remote-browser/htpasswd;
    root /opt/ikun2api-remote-browser/web;
    index vnc.html;
    client_max_body_size 1m;
    access_log /var/log/nginx/ikun2api-browser-access.log ikun_browser_access;
    error_log /var/log/nginx/ikun2api-browser-error.log error;
    add_header Cache-Control "no-store, private" always;
    add_header X-Frame-Options DENY always;
    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy no-referrer always;
    location = /connect {
        auth_basic off;
        default_type text/html;
        return 200 '<!doctype html><html lang="en"><meta charset="utf-8"><title>Remote browser</title><img src="/app/images/info.svg" width="32" height="32" alt="Remote browser"><p><a href="/vnc.html?autoconnect=1&amp;resize=remote&amp;path=websockify">Open remote browser</a></p><p>Authentication required.</p></html>';
    }
    location = /websockify {
        if ($http_origin != "IKUN_BROWSER_ORIGIN") { return 403; }
        proxy_pass http://127.0.0.1:6082;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_buffering off;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
    location / {
        try_files $uri $uri/ =404;
    }
}
'''.replace('IKUN_BROWSER_HOST',server_name).replace('IKUN_BROWSER_ORIGIN',browser_origin).replace('IKUN_BROWSER_CERTIFICATE',settings['tls_certificate']).replace('IKUN_BROWSER_PRIVATE_KEY',settings['tls_certificate_key']))
link = Path('/etc/nginx/sites-enabled/ikun2api-browser')
if not link.exists():
    link.symlink_to('/etc/nginx/sites-available/ikun2api-browser')
run(['nginx','-t'])
run(['systemctl','daemon-reload'])
run(['systemctl','enable','--now','ikun2api-browser-firewall','ikun2api-browser-proxy','ikun2api-browser','ikun2api-browser-web'])
run(['systemctl','reload','nginx'])
run(['ufw','allow','9443/tcp','comment','IKUN2api remote browser HTTPS'])
print(json.dumps({'configured':True,'url':credentials['url'],'browser_uid':uid,'proxy_source':'private settings file','services':['ikun2api-browser-firewall','ikun2api-browser-proxy','ikun2api-browser','ikun2api-browser-web']}))
