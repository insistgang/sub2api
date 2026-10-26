# 管理员 API Key 的用户并发豁免

模型请求的用户级并发策略取决于现有鉴权得到的 API Key 所属用户。
只有 `APIKey.User.Role == admin`，且 Key 的所属 ID、User.ID 与当前鉴权主体一致时，
才跳过 `users.concurrency` 对应的用户槽位及用户等待队列。ID 只用于校验所属关系，没有特定用户白名单。
缺少用户信息、角色未知或所属关系不一致时，继续执行原来的用户限制。

不修改用户数据、默认并发数或上游账号配置，也不把各分组容量相加。
同一个管理员的所有 API Key 都应用此策略；用户名、邮箱、Key 名称和请求体中的角色字段不参与判断。
角色仍由原来的鉴权和缓存机制提供，没有新增鉴权方式或调整缓存协议。

## 路径覆盖

| 路径 | 用户级入口 |
|---|---|
| Anthropic Messages、兼容 Chat Completions／Responses、SystemOne | 共享 `AcquireUserSlotWithWait` |
| OpenAI Responses、Chat Completions、Messages、Embeddings、Images、Alpha Search | `acquireResponsesUserSlot` → 共享入口 |
| Gemini 模型操作及显式缓存创建 | 共享入口 |
| Grok 图片／视频适用入口 | 共享入口 |
| 异步图片执行 | 复制后的 Gin 上下文进入上述图片 handler，保留原 Key 所属角色 |
| Responses WebSocket | 首轮、失败后重取、后续轮次均显式传入已鉴权 API Key |
| OpenAI Live | 创建请求的临时用户槽位及持续 Live 租约中的用户预算 |

Live 的内部 `OwnerRole` 不接受 JSON 输入，也不序列化到外部。
管理员在持续租约调用边界使用缓存接口已有的 `userMax=0` 语义来略过用户预算检查；
这不写回 `users.concurrency`，账号上限、账号／用户／Key 的租约记录和续租仍然保留。

原本没有用户槽位的路径不新增限制，例如 Grok 音频／实时音频／搜索、部分 token 计数、批量图片提交。

## 保持有效的保护

- 账号级 `AcquireAccountSlot`、账号等待队列、跨分组调度与故障切换。
- 用户／分组／账号 RPM、上游 429／529、额度、余额、订阅、计费和 API Key 窗口限额。
- WebSocket 独立入站连接租约、Live 账号租约、图片全局并发保护。
- API Key 并发统计的登记与释放。

管理员不会因为用户槽位或用户等待队列而得到 `gateway_concurrency_limit`／`gateway_queue_full`。
账号满额或其他独立保护仍可排队、拒绝或返回相关错误码；本策略不会抢占其他请求。
非管理员继续使用原来保存的并发数、等待算法和默认配置。

管理员的普通模型请求不占用用户层槽位，运维时应结合 API Key 与账号槽位统计观察实际负载，
不要把用户资料中的并发数字当作管理员的有效模型并发上限。

## Redis 多实例与启动清理

不同应用实例通过同一 Redis 存储及同一账号 ID 共享账号上限，分组不会产生独立的重复容量。
正常启动只调用现有的过期清理，不再根据随机进程前缀删除其他实例的活跃槽位或等待计数。
旧的前缀清理 API 保留兼容，但不用于共享 Redis 的正常启动。
崩溃遗留状态依靠已有 TTL 和回收机制消退；TTL、获槽脚本与排队参数没有改变。

## 验证

针对性测试覆盖管理员／普通用户、角色和所属 ID 不匹配、多个 Key、鉴权缓存命中、
用户队列满、WebSocket 各轮重取、Live 用户预算，以及账号已满时仍拒绝。
两个独立客户端／服务共享 miniredis，验证跨实例及跨分组账号容量、正常释放和启动保护。
可选真实 Redis 测试自行启动临时 Unix socket 实例，不连接任何已有 Redis。

```sh
cd backend
GOMAXPROCS=2 make test-unit
SUB2API_TEST_REDIS_BINARY=/path/to/redis-server GOMAXPROCS=2 \
  go test -tags=unit ./internal/repository \
  -run TestAdminConcurrencyAccountCapsWithDisposableRedis -count=1 -v
```
