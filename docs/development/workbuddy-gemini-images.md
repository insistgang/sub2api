# WorkBuddy 的 Gemini 原生生图工具

`scripts/workbuddy_gemini_image_mcp.py` 提供独立的 stdio MCP 服务，使用 Sub2API 已有的
Gemini 原生 `generateContent` 接口，把 `inlineData` 保存为本地图片并返回 MCP 图片预览。
不需要新增公网监听端口，也不修改已有的 Image25Flare 工具或聊天模型。

## 接口与模型名称

Gemini 分组使用 `/v1beta/models/{model}:generateContent`；Antigravity 分组使用
`/antigravity/v1beta/models/{model}:generateContent`。分组必须启用生图权限。

工具名为 `generate_gemini_image`，接收 `prompt`、`model`、`aspect_ratio`、`image_size`。
后两个参数默认 `1:1`、`1K`，尺寸参数也接受 `2K` 和 `4K`。实际输出尺寸由上游决定。

允许的四个请求名称是：

- `gemini-3.1-flash-image`
- `gemini-3.1-flash-image-preview`
- `gemini-3-pro-image`
- `gemini-3-pro-image-preview`

**请求名称不等于实际模型。** 本分支的默认 Antigravity 映射把上述四个名称全部映射到
`gemini-3.1-flash-image`；这是兼容映射，不代表存在四个独立后端。账号设置显式映射后
行为可能不同。工具结果同时保留 `model` 和上游的 `modelVersion`（`upstream_model`），
应结合网关的 `upstream_model` / `upstream_response_model` 记录核验。不要仅凭 HTTP 200
宣称 Pro 后端已验证。

Google 的 [Gemini 图像模型文档](https://ai.google.dev/gemini-api/docs/image-generation)
描述官方模型能力；具体账号的权限、实际路由与额度必须另行实测。

## WorkBuddy 配置

把下面的条目合并到现有 `mcpServers`，替换示例路径、域名与认证来源模型 ID。
先备份原始配置，保留其他条目。脚本只使用 Python 标准库。

```json
{
  "gemini-images": {
    "type": "stdio",
    "command": "/absolute/path/to/python3",
    "args": ["-u", "/absolute/path/to/sub2api/scripts/workbuddy_gemini_image_mcp.py"],
    "env": {
      "WORKBUDDY_MODELS_CONFIG": "/absolute/path/to/.workbuddy/models.json",
      "GEMINI_IMAGE_AUTH_MODEL": "existing-gemini-chat-model-id",
      "GEMINI_IMAGE_BASE_URL": "https://gateway.example/antigravity/v1beta",
      "GEMINI_IMAGE_OUTPUT_DIR": "/absolute/path/to/Pictures/Gemini Images"
    },
    "timeout": 300000
  }
}
```

脚本每次调用时读取认证来源模型的现有 `apiKey`，不将密钥复制进 MCP 配置、仓库或工具
返回值。网关地址必须与该模型 URL 使用相同的 HTTPS 来源；禁止跟随 HTTP 重定向。
删除或重命名认证来源模型后，需要相应更新 `GEMINI_IMAGE_AUTH_MODEL`。

在 WorkBuddy 的“连接器 → 自定义连接器”中保存配置，让应用重新加载服务；首次连接
需要应用的信任确认。成功时显示 `gemini-images` 的 `1/1 个工具已启用`。
正常使用时可说：“调用 generate_gemini_image，用 gemini-3.1-flash-image 生成一张蓝色
茶壶的图片，1K，正方形。”图片会保存到配置的目录，并在工具结果中返回路径和预览。

## 验证与当前限制

离线测试：

```sh
python3 -m unittest discover -s scripts -p 'test_workbuddy_gemini_image_mcp.py' -v
```

覆盖请求参数、最终图片与思考图片区分、无图/损坏数据拒绝、文件与预览字节一致性、
文件权限、凭据来源校验、禁止重定向，以及真实 stdio 的初始化和工具发现。

2026-10-24 的部署验证：

- 原生接口成功返回 `gemini-3.1-flash-image` JPEG，13.48 秒，实际 1024×1024；
  已解码并检查图片内容。
- WorkBuddy 配置中的 MCP 命令初始化成功，工具发现包含四个名称；8 项离线测试通过。
- 随后的 MCP 实际生图测试遇到上游 `QUOTA_EXHAUSTED`：Flash 请求返回 429，其他名称因
  指向同一已限流后端而返回 503。因此该轮**不能算作四个模型经 MCP 完成出图**。
- 上游当时报告恢复时间为 `2026-10-24T13:35:48Z`（北京时间 21:35:48）。这是该次观测，
  不保证后续可用性。未清除限流记录、未自动重试生图、未设置定时任务。
- 同日 20:05 北京时间再次直接查询 Google 的 prod 和 daily 入口，两个入口都报告
  `gemini-3.1-flash-image` 剩余比例 `0.9427524`（约 94.3%），配额查询中的刷新时间为
  `13:14:54Z`。20:06 对两个生成入口各做一次独立诊断，均返回 429；daily 仍报告
  `QUOTA_EXHAUSTED` 和 `13:35:48Z` 恢复时间。诊断未经过 Sub2API 的本地限流预检查，
  因而可以确认上游当时确实拒绝生成，但**不能把这等同于账户剩余额度为零**。
  额度查询与生成接口的口径存在不一致；额外的时间窗口、容量限制或上游报告异常均未被
  单独证实。未据此清除本地限流记录或切换生产入口。
- 当前仅完成 1K 正方形的实时图像验证，2K/4K 和其他比例尚未实测。

机器专属配置、备份、完整测试证据及产物保存在仓库之外。回滚时只移除新增加的
`gemini-images` 条目，或在确认没有后续修改后恢复原备份；不覆盖其他新增连接器。
