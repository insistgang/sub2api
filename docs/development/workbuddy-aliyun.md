# WorkBuddy 接入与阿里云分阶段升级

## 目标与验收

在本机维护可独立开发的 Sub2API fork，接入现有 WorkBuddy 配置，修复可复现的问题，
并将经过验证的版本迁移到现有阿里云部署。整个过程必须保留账号、API key、配置、
用量数据和回滚能力。

只有下列每一项都有证据，才能宣布整个迁移完成。`/health` 返回正常，并不能单独证明
认证、模型推理、工具调用或流式响应可用。

- [x] 建立所有者的 GitHub fork，并在本机检出；配置 `origin` 和 `upstream`。
- [x] 将开发基线固定为截至 2026-10-23 已核实的最新稳定版：`v0.2.18`，提交
  `ac1b8f1271be0d674fa6f074403c1410f30a3a81`。
- [x] 完成当前 WorkBuddy 配置清点，覆盖全部模型、推理与能力设置、MCP 工具、已启用
  插件和故障切换配置。
- [x] 确立本地构建、测试和开发命令。
- [ ] 复现并修复当前连接和 GPT 故障，提供回归证据；区分客户端、代理、网关、账号和
  上游问题。
- [ ] 审计现有生产部署，并保存可恢复的私有备份。
- [x] 在隔离的本地 staging 完成备份恢复、版本升级验证和数据保留核验（详见下文）。
- [ ] 在受控切换中构建并部署经过审查的 fork 到阿里云。
- [ ] 验证部署版本、持久化、现有客户端、配置的模型路由、流式响应、工具调用、媒体
  集成和回滚流程。
- [ ] 明确记录剩余限制；未完成的检查不得标记为通过。

## 当前证据与假设

用户授权完成整个流程，包括修复和分阶段迁移。以最新稳定版作为可复现基线，并保留
上游 remote 以便后续同步。现有个人配置应作为需要保留和核验的输入，不得发布到这个
公开 fork。

最初检查时，旧本地检出在 `v0.2.17` 且工作区干净；生产报告使用 `0.2.10` 镜像、
PostgreSQL 15、Redis 7，并挂载持久化目录 `/app/data`。应用变更前要重新核对线上状态。
历史迁移报告只能作为背景，不能证明当前服务健康。不要重放已完成的迁移辅助脚本，也
不要重新启用过期的 OAuth 账号副本。

WorkBuddy 5.8.3 配置了 15 个自定义模型，模型配置中的 URL 均指向 Chat Completions；
另一个原生客户端使用 Responses。媒体 MCP 工具使用独立端点。除非已核实的问题需要
明确且有记录的修正，否则保留用户的模型选择和推理偏好。近期本机日志中的 503、502、
504 和超时没有关联到模型 ID、`/v1` 路径或自定义 Sub2API 主机，不能仅凭这些本机日志
归因。随后生产日志确认了计费服务 503、容器 DNS 和上游 TLS 超时，并测得严重内存压力。
增加 2 GiB swap、将 `vm.swappiness` 从 0 调为 60 后，一分钟内存完全停顿率从约 84%
降至 1% 以下，公网健康检查约 70 ms；GPT-6 Luna、DeepSeek、Gemini、Grok、Kimi 的
代表性文本请求通过。完整模型、流式和工具调用验收仍须在生产切换后完成。

私有配置快照、账号导出、日志、凭据和机器专属证据应保存在仓库之外的所有者专用目录，
例如 `~/.local/share/sub2api-dev/`。这些文件不得提交到 Git，也不能以“只是开发分支”
为由提交到公开 fork。

## 仓库结构与命令

- `backend/`：Go 服务、数据库迁移和 Go 测试。
- `frontend/`：Vue 应用和 Vitest 测试。
- `deploy/`：部署配置和部署契约测试。
- `docs/development/`：可复现的开发与迁移说明。

除非命令明确切换目录，否则从仓库根目录运行：

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

以上命令是仓库提供的开发入口。2026-10-23 的本地结果见下文；不要把未实际运行的
命令推定为已通过。环境记录应注明实际使用的工具链版本。集成测试需要按文档准备数据库、
容器和隔离测试数据。

## 工作顺序

1. 清点当前客户端配置和生产状态，确认哪些路由可用、哪些失败；报告中只保存脱敏证据。
2. 建立本地开发环境。在把故障归因于下游改动前，先验证未修改的发布基线。
3. 每次只修复一个已复现的问题。代码缺陷先增加失败行为测试，再做最小修正，并运行
   相关回归测试。
4. 使用新备份准备 staging。审查生产版本到候选版本之间的全部迁移，并在升级后核对
   数据和权限。
5. 只有 staging 通过后才能切换。保留旧镜像以及配套的数据库和数据目录备份。仅回退
   容器镜像不等于回滚数据库。
6. 部署后验证真实客户端路径，并填写最终证据矩阵。

## 边界与验证要求

遵循现有 Go/Vue 约定，改动范围限于已观察到的问题。测试应断言外部可见行为，例如：
上游失败时返回恰当的协议错误，而不是空成功。不能只因为显示名称不同就替换可用的模型 ID。

修改配置或部署前先备份。保留数据库卷、加密设置、有效 API key、OAuth 所有权和持久化
数据挂载。staging 不得与生产环境并行刷新或消耗 OAuth 账号池。不得公开密钥、臆测性地
重置凭据、关闭 TLS 验证或删除无关工作。

最终矩阵必须区分：已配置、静态验证、运行时验证、失败和未测试。每项通过的要求都应
记录准确命令、构建标识、时间戳和脱敏输出。

## 已验证的本地开发实例

Mac 上的开发实例使用全新数据库，不是生产数据的副本。UI 地址为
`http://127.0.0.1:19080`。PostgreSQL 15 只监听 `127.0.0.1:15432`；本地开发用
Redis 8 只监听 `127.0.0.1:16379`。生产使用 PostgreSQL 15 和 Redis 7，两者的升级
兼容性必须单独验证。这个全新本地数据库不能证明生产数据恢复成功。

本地私有目录为 `~/.local/share/sub2api-dev/local/`。本地管理员账号是
`admin@sub2api.local`；自动生成的密码保存在仅所有者可读的 `secrets.json` 中。
`server.env.json` 保存此实例的环境变量。这两个文件都不能提交到 Git。当前进程和日志
分别记录在 `server.pid` 与 `server.log`。

应用通过 `local-isolated.sb` 运行；该配置只允许连接两个数据库端口。已确认外部地址和
本机代理端口均被拒绝，避免隔离开发配置下的后台任务访问生产 provider。

安装的 Go 工具链位于 `~/.local/share/sub2api-dev/toolchains/go1.27.3/go/`。从仓库
根目录重新构建本地嵌入式服务：

```sh
env PATH="$HOME/.local/share/sub2api-dev/toolchains/go1.27.3/go/bin:$PATH" \
  GOMAXPROCS=2 make -C backend test-unit
pnpm --dir frontend run build
cd backend
env PATH="$HOME/.local/share/sub2api-dev/toolchains/go1.27.3/go/bin:$PATH" \
  GOMAXPROCS=2 go build -tags embed -p 2 -trimpath \
  -ldflags '-s -w -X main.Version=0.2.18-dev' -o bin/server-local ./cmd/server
```

停止当前本地应用后，从仓库根目录运行以下命令，重启已完成初始化的实例。命令会直接将
密钥读入子进程环境，不会打印密钥：

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

如果本地数据库进程已停止，应先按专用配置启动；不要使用其他 Homebrew 默认数据目录：

```sh
/opt/homebrew/opt/postgresql@15/bin/pg_ctl \
  -D "$HOME/.local/share/sub2api-dev/local/postgres" \
  -l "$HOME/.local/share/sub2api-dev/local/postgres.log" -w start
/opt/homebrew/bin/redis-server "$HOME/.local/share/sub2api-dev/local/redis.conf"
```

2026-10-23 的验证结果：前后端构建均通过；后端 57 个单元测试包全部通过；选定的
30 个前端测试文件共通过 575 项测试。本地运行实例应用了 297 个迁移文件，其中包含
241–246 范围内的 8 个新增迁移文件；health endpoint、HTML 应用外壳和管理员登录均
通过。这些结果只适用于全新的本地开发实例。

## 已恢复的本地 staging

这个 staging 与上面的全新开发实例分开：它从生产备份恢复数据，用于验证迁移和数据保留，
不是生产环境，也不是生产切换。迁移数从 289 增至 297。恢复后，9 个账号凭据哈希、
11 个 key 的值与绑定、2 个余额均保留；11 个 key 均在 staging 完成认证。staging 的
provider 账号已禁用，外部网络访问已阻断。

这些结果证明本地隔离 staging 的恢复和验证通过；不代表生产部署已经升级或切换。生产
审计、备份复核、受控切换和上线后验证仍是待完成门槛。

## 可重复运行的模型检查

`scripts/workbuddy_smoke.py` 会读取当前 WorkBuddy 模型文件。默认执行 dry run；
`not_run` 绝不会被报告为模型请求成功。实时检查会为每个选定模型发送一条简短文本请求
或诊断函数请求。诊断函数不会执行；图像和视频生成不在检查范围内。

```sh
python3 scripts/workbuddy_smoke.py
python3 scripts/workbuddy_smoke.py --live --model MODEL_ID --timeout 60
python3 scripts/workbuddy_smoke.py --live --model MODEL_ID --stream --tool-call
python3 -m unittest discover -s scripts/tests -p 'test_workbuddy_smoke.py' -v
sh deploy/tests/docker-go-toolchain-test.sh
```

`--timeout` 是 socket 超时；流式请求另有经过时间检查，但它不是严格的进程截止时间。
显式指定 `--output` 时会写出权限为 mode 600 的脱敏 JSON 报告。检查器会拒绝重定向、
HTTP 200 中的错误响应体、空响应、不完整流和无效的诊断工具参数。17 个 fixture 测试
已通过。生产模型的实时验收仍是单独的门槛。

用户选择保留 MiniMax 配置；待配额恢复后再运行相应的实时检查。在配额恢复前，不能把
MiniMax 实时检查标记为通过。

## 升级专项检查

应用镜像升级和数据存储主版本升级要分别处理。不要在现有部署上运行安装 bootstrap，
因为它会生成新密钥。保留持久化应用数据挂载及当前加密/JWT 设置。迁移 246 会开启
现有 Gemini/Antigravity 第 1、7 组的图像生成标志（`false` → `true`）；升级后要有意
核对权限变化，包括复合组。

已在一次性 Redis 7.4.11 容器中验证新增命令和 6 个 Lua 脚本。此项只证明这些 Redis
操作在该版本容器中的验证结果，不等于完成生产升级或生产数据迁移。开发实例使用
Redis 8；生产数据恢复验证也仍与生产切换分开。

构建生产 Docker 镜像时，必须显式传入 `VERSION` 和 `COMMIT`。v0.2.18 发布树的
VERSION fallback 文件仍写着 `0.2.17`，且 Docker 构建上下文排除了 `.git`。下游
Docker 工具链契约测试会检查三份受支持的 Dockerfile，避免它们声明的工具链偏离 `go.mod`。

对于资源有限的生产主机，可在本地编译嵌入式 Linux/amd64 二进制，再用
`deploy/Dockerfile.prebuilt` 打包。将 `RUNTIME_IMAGE` 固定为已验证且匹配的上游镜像
digest，不要使用会变化的 tag。最终镜像保留上游运行库、PostgreSQL 工具、资源文件、
entrypoint 和 health check，只替换为 fork 编译出的服务二进制。镜像的
VERSION/COMMIT/BUILD_DATE 参数必须与二进制内嵌值一致。部署前核对镜像标签和
`/app/sub2api --version`。这样可避免在生产主机上运行 Node 和 Go 编译。

## 按 Provider 选择错误规则

兼容 OpenAI 的路由也可能转发到非 OpenAI Provider。最终 failover 应像非 failover 路径
一样，按实际失败账号所属的 Provider 应用错误规则。只有缺少 Provider 上下文的错误才
继续使用旧版 OpenAI fallback。回归覆盖必须证明：仅针对 MiniMax 的规则会匹配 MiniMax
故障；仅针对 OpenAI 的规则不会匹配该故障；真实 OpenAI 行为和旧版兼容行为仍保持正常。
这项改动只调整规则选择，不改 Provider 配额或默认 HTTP 状态映射。
