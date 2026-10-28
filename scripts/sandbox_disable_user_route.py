#!/usr/bin/env python3
"""On the 155 host, return AgentAPI to the saved fake-only canary users."""
from pathlib import Path
import os
import subprocess
import time
import urllib.request

CONFIG = Path('/opt/agentapi/config.env')
BACKUP = Path('/opt/agentapi/sandbox-canary-build/config.before-real-user-routing.env')


def main():
    if not BACKUP.is_file() or not CONFIG.is_file():
        raise SystemExit('saved canary configuration is missing')
    saved = BACKUP.read_text()
    if 'AGENT_API_SANDBOX_CANARY_USER_IDS=sandbox-smoke-user' not in saved:
        raise SystemExit('backup is not the expected fake-only canary')
    CONFIG.write_text(saved)
    CONFIG.chmod(0o600)
    command = [
        'docker', 'compose', '--env-file', str(CONFIG), '--env-file',
        '/opt/agentapi/secrets.env', '-f', 'deploy/local/docker-compose.yml',
        'up', '-d', '--no-build', '--no-deps', '--force-recreate', 'agentapi',
    ]
    env = os.environ.copy()
    env['AGENT_API_IMAGE'] = subprocess.check_output(
        ['docker', 'inspect', 'local-agentapi-1', '--format', '{{.Config.Image}}'],
        text=True,
    ).strip()
    if not env['AGENT_API_IMAGE']:
        raise SystemExit('running image could not be determined')
    result = subprocess.run(command, cwd='/opt/agentapi/repo', env=env, timeout=180)
    if result.returncode != 0:
        raise SystemExit('API recreation failed; safe fake-only config remains on disk. Inspect service before retrying')
    for _ in range(30):
        try:
            with urllib.request.urlopen('http://127.0.0.1:8081/readyz', timeout=3) as response:
                if response.status == 200:
                    print('real user sandbox route disabled; API healthy')
                    return
        except Exception:
            pass
        time.sleep(3)
    raise SystemExit('API did not become healthy; operator attention required')


if __name__ == '__main__':
    main()
