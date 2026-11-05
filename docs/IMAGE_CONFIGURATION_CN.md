# 生图与常用配置指南

本文面向自部署管理员和 API 使用者，说明从账号、分组到客户端调用的完整配置链路。示例域名均为占位符，不要原样作为生产地址。

## 1. 先选对生图方式

- **OpenAI 兼容单次生图/编辑**：调用 `/v1/images/generations` 或 `/v1/images/edits`，适合普通客户端；同步 Images 路由支持 OpenAI、Grok 分组。下面的 GPT Image 示例使用 OpenAI 分组。
- **单次异步生图/编辑**：调用上述地址的 `/async` 版本，提交后轮询任务；需要 Redis 和完整的对象存储配置，详见第 6 节。
- **Gemini 批量生图**：调用 `/v1/images/batches`，不是单次异步接口；需要单独配置批量账号、队列和计价，见 [批量图片说明](BATCH_IMAGE_MVP.md)。
- **Gemini / Antigravity 原生接口**：不要因为账号能调用文本模型，就把该平台密钥直接用于 OpenAI Images 接口。需要使用对应平台原生协议，或另行配置支持 Images 的路由。

`/health` 返回成功只表示应用存活，不表示账号有图片额度、白名单正确或图片上游可用。最终验收需要实际生成一张图片；这会产生上游调用和费用。

## 2. 管理员配置顺序

### 2.1 添加上游账号

在管理后台的账号管理中选择 OpenAI 平台，并按上游实际凭证类型添加账号：

- **API Key 账号**：填写上游 Key、Base URL；上游必须真正实现 Images 接口并有图片模型权限。不要把 Sub2API 用户 Key 填成上游 Key，也不要把 Base URL 指回本实例造成循环请求。
- **OAuth / Setup Token 账号**：使用该账号实际具有的图片能力；订阅等级、图片额度和风控限制仍由上游决定，网关不能通过改模型名解除限制。
- 账号需要出站代理时，在代理管理中添加代理，再给账号选择对应代理。代理地址必须从后端运行环境可达；容器中的 `127.0.0.1` 是容器自己，不是宿主机。

将账号加入预期分组，确认账号启用且可调度。后台测试成功后，再用用户 API Key 测试完整链路。

### 2.2 配置分组、白名单和映射

1. 创建或选择 OpenAI 分组，把有图片能力的账号绑定进去。
2. 若分组有模型白名单，加入客户端要请求的图片模型。
3. 若账号有显式模型映射/白名单，也加入相应模型。例如需要原样透传时，使用同名映射：`gpt-image-2.5-sunburst → gpt-image-2.5-sunburst`。
4. 若使用渠道或复合分组，同时检查该层的模型限制、路由目标和计价。
5. 在用户端创建绑定上述分组的 API Key，并确认余额、订阅、Key 配额及 IP 限制允许调用。

升级程序不会自动扩大已有显式白名单。不要混选 OpenAI、Gemini、Antigravity 账号批量覆盖模型映射；不同平台的模型和协议不能互相替代。

### 2.3 模型支持以运行版本为准

当前仓库的 OAuth 原生 Codex Images 直调名单包含：

```text
gpt-image-1.5
gpt-image-2
gpt-image-2.5-flare
gpt-image-2.5-sunburst
gpt-image-2.6-flare
gpt-image-2.6-sunburst
gpt-image-2.5-flare-2026-09-08
gpt-image-2.5-sunburst-2026-09-08
```

以上是网关的路由名单，不是所有账号都具备这些模型权限的承诺。API Key 透传账号还取决于其上游接口支持。

**版本与发布注意**：`0.2.18-workbuddy.13-image26` 的 2.6 原生直调及价格兜底补丁已同步到当前工作区源码；仍需提交、构建并按发布流程部署，旧镜像不会自动更新。此前线上发布记录中 Sunburst 生成和编辑已验证，Flare 尚未实测。新构建仍需核对账号白名单、实际出图和扣费，不能只凭模型名或镜像标签判断支持情况。预编译打包使用 `deploy/Dockerfile.prebuilt` 时必须提供 `VERSION`、`COMMIT`、`BUILD_DATE`、`BINARY_SHA256`，这些值必须与二进制一致，构建会校验 SHA-256 和 `--version` 输出。

## 3. 客户端怎么填

在支持 OpenAI Images 的客户端中填写：

```text
Base URL: https://api.example.com/v1
API Key:  在本项目用户端创建的 API Key
图片模型: gpt-image-2.5-sunburst
```

某些客户端会自行追加 `/v1`，此时 Base URL 填 `https://api.example.com`，避免出现 `/v1/v1/images/generations`。只支持 Chat Completions 的聊天客户端不一定支持图片生成，填写图片模型名不会自动增加接口能力。

以下 curl 示例的 `BASE_URL` 已包含 `/v1`。不要把真实 Key 写进文档、截图或 Git；在自己终端中安全设置 `SUB2API_API_KEY` 环境变量后再执行。请求会消耗额度。

### 3.1 文生图

```bash
export BASE_URL='https://api.example.com/v1'

curl --fail-with-body --max-time 900 "$BASE_URL/images/generations" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-image-2.5-sunburst",
    "prompt": "一座海边灯塔，水彩画风，不要文字",
    "size": "1024x1024",
    "quality": "auto",
    "n": 1,
    "response_format": "b64_json"
  }' > generation.json
```

`curl` 成功后还要检查 JSON：正常图片通常在 `data[].b64_json` 或 `data[].url`，不能仅凭 HTTP 200 判定出图成功。下面将第一张 base64 图片保存到文件；若返回 URL，应按 URL 下载，而不是把 JSON 文件改名为 PNG。

```bash
python3 - <<'PY'
import base64
import json
from pathlib import Path

result = json.loads(Path('generation.json').read_text())
image = result['data'][0]
if image.get('b64_json'):
    Path('generated.png').write_bytes(base64.b64decode(image['b64_json'], validate=True))
    print('已保存 generated.png（默认图片格式；若请求了其他格式，请使用对应扩展名）')
elif image.get('url'):
    print('请下载 data[0].url 中的图片；链接可能有有效期。')
else:
    raise SystemExit('响应没有图片，请检查错误字段和上游日志。')
PY
```

先用 `n=1`、标准尺寸验证，再增加数量、质量或尺寸。`quality`、`size`、`output_format` 等参数是否可用仍受具体上游和模型约束；不要将某个模型的参数集合套给所有模型。

### 3.2 参考图编辑

将当前目录的 `reference.png` 作为参考图。multipart 请求让 curl 自行生成 Content-Type，不要手工填写 `application/json`。

```bash
curl --fail-with-body --max-time 900 "$BASE_URL/images/edits" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -F 'model=gpt-image-2.5-sunburst' \
  -F 'prompt=保留主体和构图，将背景改为雪山，保持自然光照' \
  -F 'image=@reference.png;type=image/png' \
  -F 'size=1024x1024' \
  -F 'response_format=b64_json' > edit.json
```

当前 OAuth 图片处理路径对单个上传部分设有 20 MiB 上限；反向代理和整体请求体限制也可能更严格。尽量使用较小的 PNG/JPEG/WebP，并按实际模型要求选择格式。多参考图、mask 和自定义尺寸请先验证上游支持，再扩展请求。

## 4. 图片模型与文本主控模型不是同一项

- Images 请求体中的 `model` 选择**图片模型**。
- `SUB2API_IMAGES_MAIN_MODEL` 选择 OAuth **Responses 工具桥接路径**使用的文本主控模型，默认 `gpt-5.6-luna`。
- 对第 2.3 节明确列出的模型，OAuth 使用原生 Codex Images 直调，**不会经过该文本主控模型**。因此修改主控环境变量不能修复直调模型的权限或额度问题。
- 非直调模型走 Responses 桥接时，主控模型需要是账号支持的文本模型；该设置不会把图片模型替换成文本模型，也不覆盖客户端 `/v1/responses` 已提供的文本主控模型。

例如仅在确认桥接路径主控已不可用时，才把 `SUB2API_IMAGES_MAIN_MODEL` 改成该账号可用的文本模型。不要为了解决所有生图错误而盲目修改这一项。

## 5. 部署配置改在哪里、什么时候生效

先区分三类配置：

- **后台业务设置**：账号、代理、分组、模型映射、价格、对象存储等；通过后台保存，按各功能规则生效。
- **后端启动配置**：部署环境变量和 `config.yaml`；通常需要重建容器或重启进程。
- **前端开发配置**：如 `VITE_DEV_PROXY_TARGET`；只影响本地 Vite 开发代理，不会改变生产后端的上游地址。

### 5.1 Docker Compose

`.env` 是 Compose 的变量替换来源，不代表文件内的每个变量都会自动进入容器。必须同时存在对应的服务 `environment:` 配置。仓库 Compose 已传入下面列出的 `GATEWAY_IMAGE_*` 和 `SUB2API_IMAGES_MAIN_MODEL`；自行新增环境变量时要检查是否也传入服务。

修改自己的部署目录 `.env` 后，只更新应用服务（`sub2api` 是仓库默认服务名）：

```bash
docker compose up -d --no-deps sub2api
```

`docker compose restart` 不会应用修改后的容器环境变量。若原部署用了 `-p`、多个 `-f` 或镜像 override 文件，必须保持相同参数，不能换用仓库默认 Compose，否则可能启动另一套环境或覆盖定制镜像。切勿为应用配置变更执行 `down -v`，它会删除数据卷。

二进制部署则在服务实际读取的 `config.yaml` 或服务环境中配置，按原服务管理方式重启。不要把 `.env` 当成二进制自动加载文件；尤其 `SUB2API_IMAGES_MAIN_MODEL` 是进程环境变量，不是任意同名 YAML 字段。

### 5.2 图片保活与并发

以下是可选的保守配置示例，**不是项目默认并发值**。应根据账号额度、机器内存和实际耗时调整：

```dotenv
GATEWAY_IMAGE_STREAM_DATA_INTERVAL_TIMEOUT=900
GATEWAY_IMAGE_STREAM_KEEPALIVE_INTERVAL=10
GATEWAY_IMAGE_NONSTREAM_KEEPALIVE_INTERVAL=0
GATEWAY_IMAGE_CONCURRENCY_ENABLED=true
GATEWAY_IMAGE_CONCURRENCY_MAX_CONCURRENT_REQUESTS=2
GATEWAY_IMAGE_CONCURRENCY_OVERFLOW_MODE=reject
GATEWAY_IMAGE_CONCURRENCY_WAIT_TIMEOUT_SECONDS=30
GATEWAY_IMAGE_CONCURRENCY_MAX_WAITING_REQUESTS=10
```

- `STREAM_DATA_INTERVAL_TIMEOUT`：上游流式数据的间隔超时，单位秒，不是一次请求总时长；0 禁用，非 0 取值 60–1800。
- `STREAM_KEEPALIVE_INTERVAL`：流式保活间隔，单位秒；0 禁用，非 0 取值 5–60。代理仍必须支持流式透传。
- `NONSTREAM_KEEPALIVE_INTERVAL`：默认 0。开启后首个心跳会固化 HTTP 200，后续失败必须从响应体判断；不要用它替代异步任务解决所有 CDN 超时。
- `CONCURRENCY_ENABLED` 和 `MAX_CONCURRENT_REQUESTS`：启用进程级图片并发限制；最大并发为 0 表示不限。多副本不是共享一个全局上限。
- `OVERFLOW_MODE=reject`：满载直接拒绝；`wait` 则等待槽位，并受等待时间和等待人数限制。当前实现的等待人数为 0 表示不限制，生产建议设置正数。

图片独立并发限制不能替代用户、Key、账号或上游本身的限额。

### 5.3 反向代理和本地开发

反向代理需要允许足够的请求体和响应等待时间；SSE 路径不能被缓存或缓冲到结束。可从 [边缘安全与代理配置](../deploy/EDGE_SECURITY.md) 和仓库 [Caddyfile](../deploy/Caddyfile) 开始配置。若 CDN 有固定响应时限，单纯增大 Nginx 超时不会解除 CDN 限制，优先使用异步接口。

本地前端开发可设置 `VITE_DEV_PROXY_TARGET=http://127.0.0.1:8080` 指向自己的本地后端；若使用 SSH 隧道，则填隧道监听地址，并先用 `/health` 验证。生产迁移后，隧道仍指向旧服务器时会连接失败，不能据此判断新生产服务宕机。不要让开发页面误连生产后端后执行充值、账号修改等写操作。

## 6. 异步生图与对象存储

适合长耗时请求经过 CDN 容易超时的情况。推荐在后台 **备份 → 异步图片对象存储** 配置，确认开关、S3 Endpoint、Region、Bucket 和凭证完整。可以复用备份 S3，但应使用独立图片前缀；只有自己确实要公开图片时才设置公共访问地址。

- 后台已保存的对象存储设置优先于 `config.yaml` 的 `image_storage` 回退配置。
- 配置文件方式的完整示例见 [异步图片任务说明](ASYNC_IMAGE_TASKS.md) 和 [config.example.yaml](../deploy/config.example.yaml)。容器路径通常是 `/app/data/config.yaml`。
- 若选择 `IMAGE_STORAGE_*` 环境变量方式，需要自己在 Compose 服务中传入相应变量；只向 `.env` 增加这些值并不够。
- 关闭开关或配置不完整时，异步提交返回 404；不需要为同步生图强行启用对象存储。

沿用第 3 节的环境变量，提交：

```bash
curl --fail-with-body "$BASE_URL/images/generations/async" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-image-2.5-sunburst","prompt":"一座水彩灯塔","size":"1024x1024"}'
```

返回 `202` 后取出 `task_id`，用**提交时的同一个 API Key** 每隔约 3 秒查询：

```bash
curl --fail-with-body "$BASE_URL/images/tasks/<task_id>" \
  -H "Authorization: Bearer $SUB2API_API_KEY"
```

直到 `status` 为 `completed` 或 `failed` 后停止。任务成功时图片写入对象存储，结果返回 URL，不将大块 base64 留在 Redis。预签名 URL 会过期，请及时下载；任务状态保留期为最近状态更新后 24 小时，单任务最长执行 30 分钟。异步不是免费重试机制，不要因前端超时就反复创建新任务。

## 7. 计费怎么理解

实际费用取决于上游 usage、模型价格、分组倍率和渠道/分组的计费模式，不是仅由 `n` 或 `quality` 决定：

- 按 token 计费时，要区分文本输入、图片输入和图片输出 token；参考图编辑也可能产生输入费用。
- 按次计费时，检查对应渠道/模型的单价和规则，不能用 token 单价直接推算。
- 当前仓库对 2.5 精确模型及已列出的日期快照有内置价格兜底；自定义价格及已有计费规则仍需检查。
- 当前源码也包含 2.6 精确模型的价格兜底，与本次核对的线上补丁保持一致；旧构建不会自动获得此能力，自定义模型价格仍优先。
- 新模型上线先用一张图核对请求模型、实际路由、usage 和扣费记录，再开放批量请求。不要用删除限流、清除额度状态的方式掩盖上游配额不足。

## 8. 常见错误按层排查

| 现象 | 优先检查 |
| --- | --- |
| 401 | 是否使用用户端 API Key、Key 是否有效、Authorization 格式是否正确 |
| 403 / 模型不允许 | Key 所属分组、分组/渠道白名单、账号模型映射、IP 和用户限制 |
| 无可用账号 / 暂不可用 | 账号绑定、启用状态、实际图片权限、限流/额度、出站代理 |
| `Images API is not supported for this platform` | Key 是否绑定 OpenAI/Grok 分组，是否误用其他平台协议 |
| 异步接口 404 | 对象存储开关和完整性；轮询是否使用提交时同一个 Key |
| 429 | 区分图片进程并发、用户/账号限制与上游配额，不要一律调高并发 |
| 413 | 请求体/单图大小是否超过后端及反向代理限制 |
| 502 / 上游错误 | 查看安全的错误摘要、目标模型/端点、代理连通性，避免把凭证或完整图片写入排错记录 |
| 504 / CDN 524 | 代理等待时间、CDN 固定时限；长任务考虑异步接口 |
| HTTP 200 但没有图片 | 检查响应 error、data、base64/URL；非流式保活可能提前提交 200 |
| 修改 `.env` 无效 | 变量是否传入容器、是否重建应用、是否使用了原来的 Compose 项目和 override |
| 更新后原有 2.6 不可用 | 是否部署了包含 2.6 路由与价格兜底的新源码，以及账号白名单是否允许，不要只比较镜像名称 |

最小验收顺序：健康检查 → 后台账号测试 → 用户 Key 单张生成 → 参考图编辑 → usage/扣费核对 → 最后再启用异步、并发或批量。不要在公开日志、Issue 或截图中暴露 API Key、OAuth Token、S3 凭证或私人参考图。
