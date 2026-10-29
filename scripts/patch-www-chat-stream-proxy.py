from pathlib import Path
import shutil
import subprocess

source=Path('/opt/agentapi/nginx/www.conf')
backup=Path('/opt/agentapi/nginx/www.conf.before-chat-stream-20261029')
body=source.read_text()
marker='''  ssl_prefer_server_ciphers off;

  location / {
    proxy_pass http://127.0.0.1:8080;'''
assert body.count(marker)==1, 'unexpected HTTPS www proxy layout'
start=body.index(marker)
original=body[start:body.index('\n  }',start)]
headers='\n'.join(line for line in original.splitlines() if line.strip().startswith('proxy_set_header ') and not line.strip().startswith(('proxy_set_header Connection ','proxy_set_header Upgrade ')))
assert len(headers.splitlines())==4, 'expected four existing forwarded headers'
location='''  # Long-running chat response stream; keep outer proxy alive while the
  # background Skill job completes and emit response chunks immediately.
  location ~ ^/v1/sessions/[^/]+/messages$ {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
'''+headers+'''
    proxy_read_timeout 900s;
    proxy_send_timeout 900s;
    proxy_buffering off;
  }

'''
if 'location ~ ^/v1/sessions/[^/]+/messages$ {' in body:
    raise SystemExit('chat stream location already present')
shutil.copy2(source,backup)
source.write_text(body.replace(marker,'  ssl_prefer_server_ciphers off;\n\n'+location+'  location / {\n    proxy_pass http://127.0.0.1:8080;',1))
check=subprocess.run(['docker','exec','agentapi-www-proxy','nginx','-t'],capture_output=True,text=True)
if check.returncode:
    shutil.copy2(backup,source)
    raise SystemExit('nginx -t failed; restored old config: '+check.stderr[-500:])
reload=subprocess.run(['docker','exec','agentapi-www-proxy','nginx','-s','reload'],capture_output=True,text=True)
if reload.returncode:
    shutil.copy2(backup,source)
    subprocess.run(['docker','exec','agentapi-www-proxy','nginx','-s','reload'],capture_output=True)
    raise SystemExit('nginx reload failed; restored old config: '+reload.stderr[-500:])
print('chat stream proxy location active with 900s read timeout; backup:',backup)
