# WorkBuddy integration and staged Aliyun upgrade

## Objective and acceptance

Maintain a local, independently developable fork of Sub2API, integrate the existing
WorkBuddy configuration, repair reproducible defects, and migrate the verified
build to the existing Aliyun deployment without losing accounts, API keys,
configuration, usage data, or rollback capability.

Completion requires evidence for every item below. A healthy `/health` response
alone does not prove that authentication, model inference, tools, or streaming work.

- [x] Create the owner's GitHub fork and a local checkout with `origin` and `upstream`.
- [x] Pin the development baseline to the latest stable release verified on
  2026-10-23: `v0.2.18`, commit `ac1b8f1271be0d674fa6f074403c1410f30a3a81`.
- [x] Finish the active WorkBuddy configuration inventory, including all models,
  reasoning and capability settings, MCP tools, enabled plugins, and failover.
- [x] Establish local build, test, and development commands.
- [ ] Reproduce and repair current connectivity and GPT failures with regression
  evidence, separating client, proxy, gateway, account, and upstream failures.
- [ ] Audit the complete existing deployment and capture restorable private backups.
- [ ] Restore those backups into an isolated staging deployment; validate the
  version upgrade and preserve existing account/group/key bindings and permissions.
- [ ] Build and deploy the reviewed fork to Aliyun in a controlled cutover.
- [ ] Validate the deployed version, persistence, existing clients, configured
  model routes, streaming, tool calls, media integrations, and rollback procedure.
- [ ] Record remaining limitations explicitly; do not mark unfinished checks passed.

## Current evidence and assumptions

The user requested the whole workflow, including fixes and gradual migration.
Use the latest stable release as the reproducible baseline; retain the upstream
remote for subsequent updates. Existing personal configuration is an input to
preserve and verify, not data to publish into this public fork.

At initial inspection the older local checkout was clean at `v0.2.17`; production
reported the `0.2.10` image, PostgreSQL 15, Redis 7, and a persistent `/app/data`
mount. Re-check live state before applying changes. Historical migration reports
are useful context, not proof of current health. Do not replay completed migration
helpers or reactivate stale copies of OAuth accounts.

WorkBuddy 5.8.3 has 15 custom models. All are configured through Chat Completions;
the other native client uses Responses. Media MCP tools use separate endpoints.
Keep the user's model selection and reasoning preferences unless a verified bug
requires an explicit, documented correction.

Private configuration snapshots, account exports, logs, credentials, and machine
specific evidence belong outside the repository in an owner-only directory, such
as `~/.local/share/sub2api-dev/`. Never commit these files, even in a private branch
of this public fork.

## Repository layout and commands

- `backend/`: Go service, migrations, and Go tests.
- `frontend/`: Vue application and Vitest tests.
- `deploy/`: deployment configuration and deployment contract tests.
- `docs/development/`: reproducible development and migration instructions.

Run from the repository root unless a command specifies another directory:

```sh
git remote -v
git status --short --branch
git describe --tags --always
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend run typecheck
make test-frontend-critical
make build-frontend
make -C backend build
make -C backend test-unit
```

These are the upstream commands, not yet a claim that dependencies are installed
or that they pass on this Mac. Record actual results and required toolchain
versions as the local environment is established. Integration tests need their
documented database/container prerequisites and isolated test data.

## Work order

1. Inventory active client configuration and current production state. Establish
   which routes work and which fail; save only sanitized evidence in reports.
2. Establish the local development environment. Verify the unchanged release
   baseline before attributing failures to downstream edits.
3. Fix one reproduced defect at a time. Add a failing behavior test for code bugs,
   make the smallest correction, then run the affected regression tests.
4. Prepare staging using a fresh backup. Review all migrations between deployed
   and candidate versions, and verify data and permissions after upgrade.
5. Cut over only after staging passes. Keep the previous image and paired database
   and data-directory backups. A container-image downgrade alone is not a database
   rollback.
6. Validate real client paths after deployment and write a final evidence matrix.

## Boundaries and verification

Follow existing Go/Vue conventions and keep changes scoped to observed defects.
Tests should assert externally visible behavior, for example that a failed
upstream returns an appropriate protocol error rather than an empty success.
Do not replace working model IDs on the basis of display-name differences alone.

Back up before mutable configuration or deployment changes. Preserve database
volumes, encryption settings, active API keys, OAuth ownership, and the persistent
data mount. Staging must not refresh or consume the production OAuth account pool
concurrently. Never publish secrets, reset credentials speculatively, disable TLS
verification, or erase unrelated work.

The final matrix must distinguish: configured, statically validated, runtime
verified, failed, and not tested. Include exact commands, build identity, timestamps,
and sanitized outputs for each verified requirement.

## Verified local development instance

The Mac development instance uses a fresh database, not a copy of production.
Its UI is available at `http://127.0.0.1:19080`. PostgreSQL 15 listens only on
`127.0.0.1:15432`, and local Redis 8 listens only on `127.0.0.1:16379`.
The production PostgreSQL 15 and Redis 7 upgrade must be validated separately;
this fresh local database is not evidence that production restoration succeeded.

The private local directory is `~/.local/share/sub2api-dev/local/`. The local
administrator is `admin@sub2api.local`; its generated password is in the owner-only
`secrets.json`. `server.env.json` contains the environment for this instance.
Neither file belongs in Git. `server.pid` and `server.log` identify the current
process and its log.

The application runs with `local-isolated.sb`, which permits outbound connections
only to the two database ports. Both an external address and the local proxy port
were verified denied. This prevents background tasks from accessing production
providers while the isolated development configuration is used.

The installed Go toolchain is under
`~/.local/share/sub2api-dev/toolchains/go1.27.3/go/`. To rebuild the embedded local
server from the repository root:

```sh
env PATH="$HOME/.local/share/sub2api-dev/toolchains/go1.27.3/go/bin:$PATH" \
  GOMAXPROCS=2 make -C backend test-unit
pnpm --dir frontend run build
cd backend
env PATH="$HOME/.local/share/sub2api-dev/toolchains/go1.27.3/go/bin:$PATH" \
  GOMAXPROCS=2 go build -tags embed -p 2 -trimpath \
  -ldflags '-s -w -X main.Version=0.2.18-dev' -o bin/server-local ./cmd/server
```

After stopping the existing local app, restart the already-initialized instance
from the repository root with the following command. It reads secrets directly
into the child process environment without printing them:

```sh
python3 - <<'PY'
import json, os
from pathlib import Path
base = Path.home() / '.local/share/sub2api-dev/local'
binary = Path.cwd() / 'backend/bin/server-local'
env = dict(os.environ)
env.update(json.loads((base / 'server.env.json').read_text()))
os.chdir(base / 'app')
os.execve('/usr/bin/sandbox-exec', ['sandbox-exec', '-f',
    str(base / 'local-isolated.sb'), str(binary)], env)
PY
```

If the local database processes have been stopped, start their dedicated
configurations first; do not use the unrelated default Homebrew data directory:

```sh
/opt/homebrew/opt/postgresql@15/bin/pg_ctl \
  -D "$HOME/.local/share/sub2api-dev/local/postgres" \
  -l "$HOME/.local/share/sub2api-dev/local/postgres.log" -w start
/opt/homebrew/bin/redis-server "$HOME/.local/share/sub2api-dev/local/redis.conf"
```

On 2026-10-23, both builds passed, all 57 backend unit-test packages passed, and
the 30 selected frontend test files passed 575 tests. The local runtime applied
297 migration files, including the eight new files from 241 through 246; its
health endpoint, HTML application shell, and administrator login passed. These
results cover the fresh local instance only.

## Repeatable model checks

`scripts/workbuddy_smoke.py` reads the active WorkBuddy model file. The default is
a dry run: `not_run` is never reported as a successful model request. Live checks
send one short text or diagnostic function request for each selected model. The
diagnostic function is not executed, and image/video generation is excluded.

```sh
python3 scripts/workbuddy_smoke.py
python3 scripts/workbuddy_smoke.py --live --model MODEL_ID --timeout 60
python3 scripts/workbuddy_smoke.py --live --model MODEL_ID --stream --tool-call
python3 -m unittest discover -s scripts/tests -p 'test_workbuddy_smoke.py' -v
sh deploy/tests/docker-go-toolchain-test.sh
```

`--timeout` is a socket timeout with elapsed checks for streaming, not a hard
process deadline. An explicit `--output` writes a sanitized mode-600 JSON report.
The checker rejects redirects, error bodies returned with HTTP 200, empty
responses, incomplete streams, and invalid diagnostic tool arguments. Its 17
fixture tests pass. Live production model acceptance remains a separate gate.

## Upgrade-specific checks

Keep the application's image update separate from datastore-major upgrades.
Do not run the installation bootstrap over the existing deployment: it generates
new secrets. Preserve the persistent app-data mount and current encryption/JWT
settings. Migration 246 enables image generation for existing Gemini/Antigravity
groups; compare permissions deliberately after upgrade, including composite groups.

Use explicit `VERSION` and `COMMIT` build arguments for a production Docker image.
The v0.2.18 release tree still has `0.2.17` in its VERSION fallback file, and the
Docker build context excludes `.git`. The downstream Docker toolchain contract
test guards the three supported Dockerfiles against drifting from `go.mod`.
