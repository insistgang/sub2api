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
- [x] 复现并修复当前连接和 GPT 故障，提供回归证据；区分客户端、代理、网关、账号和
  上游问题。
- [x] 审计现有生产部署，并保存、核验可恢复的私有备份。
- [x] 在隔离的本地 staging 完成备份恢复、版本升级验证和数据保留核验（详见下文）。
- [x] 在受控切换中构建并部署经过审查的 fork 到阿里云。
- [x] 完成生产 v2 最终模型、自动流式工具、Responses、图像输入和媒体集成验证；
  部署版本、数据持久化、客户端和回滚演练均已有证据，详见下文结果栏。
- [x] 明确记录剩余限制；未完成的检查不得标记为通过。

## 当前证据与假设

用户授权完成整个流程，包括修复和分阶段迁移。以最新稳定版作为可复现基线，并保留
上游 remote 以便后续同步。现有个人配置应作为需要保留和核验的输入，不得发布到这个
公开 fork。

最初检查时，旧本地检出在 `v0.2.17`，生产使用 `0.2.10` 镜像；这些是历史状态，不是
当前生产版本。当前生产已切换到 `0.2.18-workbuddy.2`，源码提交
`76630799a19d3dc464ee8498cbd53245496f8730`，镜像 digest 为
`sha256:42babe053e3f08ca8339cf83d968c955ee5eaea8720022c747354ddc51aa0832`。源码工作分支
为 `workbuddy-aliyun-dev`，发行标签 `workbuddy-v0.2.18.2` 已推送到所有者的 fork。

本次生产切换保留了 9 个账号、11 个 key、2 个用户、8 个分组、297 个迁移、加密配置文件、
账号与分组绑定、PostgreSQL 15、Redis 7 镜像及 `/app/data` 持久化挂载。成套数据库、
应用数据和 Redis 备份已核验；v1/v2 回滚演练在隔离的 PostgreSQL 15/Redis 7 内网环境通过。
`models.json` 与 `mcp.json` 的哈希未发生变化。

备份只保存在私有位置：本地副本由维护者保存在
`~/.local/share/sub2api-dev/2026-10-23/`；生产机的 v1 和 v2 切换前备份分别位于
`/opt/sub2api-backups/pre-cutover-20261023T165649Z/` 与
`/opt/sub2api-backups/pre-cutover-20261023T180237Z/`。不要把备份或配置加密文件提交到 Git。
不要重放已完成的迁移辅助脚本，也不要重新启用过期的 OAuth 账号副本。
应用回退与完整数据恢复是两件事。本次 v2 → v1 在相同的 297 个迁移结构上已实际演练通过，
可用上一版本镜像恢复应用，并保留当前数据库和最新 OAuth 凭据。完整数据恢复则必须使用
同一时点的数据库、应用数据、Redis 和匹配镜像；不能混用 v1/v2 备份或覆盖新刷新的凭据。
下文的本地重启命令不能用于生产恢复。

历史性能诊断确认过计费服务 503、容器 DNS 和上游 TLS 超时，以及严重内存压力。增加
2 GiB swap、将 `vm.swappiness` 从 0 调为 60 后，一分钟内存完全停顿率从约 84% 降到
1% 以下，公网健康检查约 70 ms；代表性文本请求通过。这些是修复阶段的历史测量，不能
代替当前 v2 运行验证。

WorkBuddy 5.8.3 配置了 15 个自定义模型，模型配置中的 URL 均指向 Chat Completions；
另一个原生客户端使用 Responses，媒体 MCP 工具使用独立端点。用户已确认 WorkBuddy GPT
对话和工具可用。不要仅凭 `model_provider=openai` 推断实际网络路由；应以 Sub2API 的
服务端用量记录和实际探测为准。继续保留用户的模型选择和推理偏好。

MiniMax 配置按用户选择保留。配额恢复后两款 MiniMax 模型均已成功；严格的 MiniMax M3
流式工具探测完成 1 次工具调用并收到 `DONE`，耗时 3.744 秒。

生产 v2 源码回归中，后端 57 个测试包再次全部通过，新增 MiniMax fixture 案例 32 个通过；
最终实时探测为 15/15 个文本模型和 15/15 个自动流式工具调用通过。

官方迁移 246 会把 Gemini/Antigravity 第 1、7 组的图像生成标志从 `false` 改为 `true`。
这是已观察到的迁移变化，升级后要核对该权限变化；不得声称所有配置字段均保持不变。

媒体验证：WorkBuddy Image25Flare MCP 初始化、工具列出与调用成功，产出有效 PNG；请求
`1024x1024`，文件实测 `1536x1024`。该差异成因未证实；网关日志只保留入口尺寸，没有
上游请求体。用户已接受把它记录为已知限制，并明确将精确尺寸修复排除在本轮范围外，
不再生成图片，也不裁切或拉伸现有文件。Grok 视频验证生成、下载并完整解码了 3 秒、
480x480 的 MP4。

OpenAI OAuth 账号路由也已核验：Plus 账号优先级为 10，Pro Lite 为 100；一组 Plus 账号
仍在全局 5 小时限流窗口内，另一组已过 reset 并实际成功服务 `gpt-6-astra`。截至
2026-10-24 01:55 BJT（17:55 UTC），最近 15 分钟的服务端 usage 聚合为 Plus 15 次、
Pro Lite 5 次；最近的 Plus 请求为 17:53:37 UTC，Pro Lite 为 17:44:41 UTC。另一次全新
会话探测由 Plus 账号处理，HTTP 200，耗时 2.39 秒。新会话按较小的优先级数优先选择 Plus。

Sub2API 的 OpenAI 高级调度开关当前为关闭；相关分组没有启用 `gpt-6-astra` 显式模型路由。
旧会话采用 sticky 账号绑定：命中有效绑定时会先返回该账号，并将一小时 TTL 滑动续期；
quota reset 不会把已有会话自动迁移到另一个账号。因此已有 Pro 绑定的会话继续走 Pro
是当前策略，不能据此判为配额刷新故障。源码的标准 Chat Completions 日志没有记录
`sticky_session_hit`，所以不把某个未关联会话的账号选择归因到具体 Redis 绑定；本轮未读取
或导出 sticky session 标识。

`gpt-6-astra` 的账号级模型冷却字段没有记录当前 cooldown；已过 reset 的 Plus 账号在
reset 后有成功 usage。2026-10-24 01:55 BJT 检查时，三个候选账号都没有活动并发槽或等待
计数。没有发现持续的模型冷却、明确模型 pin 或当前并发占满来解释 Pro 选择。后续如要核验
具体旧会话，只用脱敏的调度/usage 元数据，不读取或公开会话标识。
Plus reset 后仍持续被选中，说明没有持续的陈旧快照把它排除；不能排除 reset 刚发生时有过
短暂缓存延迟，但这不是当前仍选 Pro 的解释。

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
make -C backend build VERSION=0.2.18-workbuddy.2
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

Mac 上当前运行的是已更新到 v2 的开发实例，使用同一源码但使用全新数据库，不是生产数据
副本。UI 地址为 `http://127.0.0.1:19080`。PostgreSQL 15 只监听 `127.0.0.1:15432`；
本地开发用 Redis 8 只监听 `127.0.0.1:16379`。生产使用 PostgreSQL 15 和 Redis 7；
一次性 Redis 7.4.11 容器已验证新增命令和 6 个 Lua 脚本。开发 Redis 8 与生产 Redis 7
必须区分记录。这个全新本地数据库不能证明生产数据恢复成功。

本地私有目录为 `~/.local/share/sub2api-dev/local/`。登录账号为 `admin@sub2api.local`，
自动生成的本地管理员密码保存在
仅所有者可读的 `secrets.json` 中；`server.env.json` 保存此实例的环境变量。这两个文件
都不能提交到 Git，文件内容也不得复制到公开报告。当前进程和日志分别记录在 `server.pid`
与 `server.log`。

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
(base / 'server.pid').write_text(str(os.getpid()) + '\n')
os.chmod(base / 'server.pid', 0o600)
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

2026-10-23 的全新本地开发实例验证结果：前后端构建均通过；后端 57 个单元测试包全部
通过；选定的 30 个前端测试文件共通过 575 项测试。本地运行实例应用了 297 个迁移文件，
其中包含 241–246 范围内的 8 个新增迁移文件；health endpoint、HTML 应用外壳和管理员
登录均通过。这些结果只适用于全新本地开发实例。

最终交付时，前端 `lint:check` 和 `typecheck` 也通过。本地运行版本已更新为
`0.2.18-workbuddy.2`，管理员登录在更新后再次验证成功。

## 已恢复的本地 staging

这个 staging 与上面的全新开发实例分开：它从生产备份恢复数据，用于验证迁移和数据保留，
不是生产环境。恢复后迁移数从 289 增至 297；9 个账号凭据哈希、11 个 key 的值与绑定、
2 个余额均保留；11 个 key 均完成认证。provider 账号已禁用，外部网络访问已阻断。

Linux v2 staging 和 v1 回滚演练均在 PostgreSQL 15/Redis 7 隔离内网通过；Linux staging
目前已停止。Mac staging 的应用和 Redis 已停止，数据库保留。备份路径见上文；生产迁移
已经完成，这些 staging 结果用于核验恢复和回滚，不代表需要再次切换生产。

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
显式指定 `--output` 时会写出权限为 mode 600 的脱敏 JSON 报告。`--tool-call` 默认使用
`--tool-choice auto`，符合已验证的常规工具模式；需要单独测试强制指定工具时使用
`--tool-choice named`。DeepSeek/Kimi 的思考模式会拒绝强制指定工具，不能把这一组合限制
误判为普通工具调用不可用。`--max-tokens` 默认 1024，可设置为 128–16384。
检查器会拒绝重定向、
HTTP 200 中的错误响应体、空响应、不完整流和无效的诊断工具参数。22 个 checker 测试
已通过。

### 生产 v2 最终结果

以下结果于 2026-10-24 完成。证据文件保存在本机私有目录
`~/.local/share/sub2api-dev/2026-10-23/`，目录日期沿用本任务开始日期。

| 检查 | 结果 | 脱敏证据文件 |
| --- | --- | --- |
| 15 个模型文本 | 15/15 通过 | `final-v2-all-models-text.json` |
| 自动工具选择＋流式终止 | 15/15 通过 | `final-v2-all-models-stream-tools.json` |
| GPT-6.1 Sol Responses | HTTP 200、completed、有效文本 | `final-v2-responses-verification.json` |
| MiniMax M3 图像输入 | HTTP 200，正确识别测试图片的蓝色物体 | `final-v2-minimax-vision.json` |
| WorkBuddy 图像 MCP | 初始化、工具清单、生成和 PNG 解码通过；尺寸限制见上文 | `image-mcp-verification.json` |
| Grok 视频 | 3.04 秒、480×480、下载和完整解码通过 | `video-verification.json` |
| 生产切换和数据保留 | 所有不变量检查通过，11 个 key 正常鉴权 | `production-deployment-v2.json` |
| 隔离 Linux staging 与回退 | 候选、上一版本和再恢复候选均通过 | `v2-linux-stage-rehearsal.json` |

本轮验收覆盖了配置审计、实际使用的网关协议和上述能力；禁用的插件不自动启用，
第三方插件的支付、通信等业务动作不在模型网关验证范围内。

MiniMax 配额恢复后，两款 MiniMax 模型均已成功。严格的 MiniMax M3 流式工具探测完成
1 次工具调用并收到 `DONE`，耗时 3.744 秒。用户已确认 WorkBuddy GPT 对话和工具可用。
原生 CUA 不可用，因此没有自动化 GUI 验收结果；不要声称已通过自动 GUI 测试。

## 发布与恢复入口

每次继续开发，先切换到 `workbuddy-aliyun-dev`，再拉取 `upstream` 的新版本并审查差异。
前端构建、后端测试、隔离数据恢复和实际协议验证通过后，再编译带有明确版本/提交的镜像。
不要直接用上游安装脚本覆盖当前部署。

生产主配置为 `/opt/sub2api/docker-compose.yml`，应用服务名为 `sub2api`。本次切换只修改
`services.sub2api.image`，随后执行配置校验和 `up -d --no-deps sub2api`；数据库、Redis、
代理及持久化目录保留。一次性操作脚本及日志保存在生产机
`/opt/sub2api-builds/0.2.18-workbuddy.2/`，已有部署 journal，不能直接重放。

如需回到上一已验证应用版本，先保存当前 compose，只把该服务镜像改为
`sub2api-local:rollback-before-0.2.18-workbuddy.2`，运行 `docker compose config --quiet`
后再重建该应用服务；不要停止或清空数据库和 Redis。回退后重新验证鉴权及真实模型请求。
这条路径已在隔离环境演练，保留的是当前数据；它不等同于把数据库恢复到历史时点。
旧版本仍有本轮修复前的 MiniMax 流式结束标记限制，应把回退用于故障恢复。

## 升级专项检查

应用镜像升级和数据存储主版本升级要分别处理。不要在现有部署上运行安装 bootstrap，
因为它会生成新密钥。保留持久化应用数据挂载及当前加密/JWT 设置。迁移 246 会开启
现有 Gemini/Antigravity 第 1、7 组的图像生成标志（`false` → `true`）；升级后要有意
核对权限变化，包括复合组。

一次性 Redis 7.4.11 容器中的新增命令和 6 个 Lua 脚本验证见上文。该兼容性检查不等于
生产升级或生产数据迁移；当前生产数据恢复、版本部署与回滚演练结果也应分别记录。

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
