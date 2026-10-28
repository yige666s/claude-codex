#!/usr/bin/env python3
"""On the 155 host: explicitly roll back the active canary sandbox policy."""
import os
import subprocess
import urllib.parse


def main():
    items = subprocess.check_output(
        ["docker", "inspect", "local-agentapi-1", "--format", "{{range .Config.Env}}{{println .}}{{end}}"],
        text=True,
    ).splitlines()
    for item in items:
        if item.startswith("AGENT_API_SQL_DSN="):
            url = urllib.parse.urlsplit(item.split("=", 1)[1])
            if url.hostname != "postgres" or url.port != 5432:
                raise SystemExit("unexpected SQL address; refusing rollback")
            host = url.netloc.replace("@postgres:5432", "@127.0.0.1:15432")
            os.environ["AGENT_API_SQL_DSN"] = urllib.parse.urlunsplit(url._replace(netloc=host))
            break
    if not os.environ.get("AGENT_API_SQL_DSN"):
        raise SystemExit("running AgentAPI SQL configuration unavailable")
    return subprocess.call(
        [
            "/opt/agentapi/sandbox-smoke/sandboxcanary",
            "-mode", "rollback",
            "-previous-revision", "3",
        ],
        env=os.environ,
    )


if __name__ == "__main__":
    raise SystemExit(main())
