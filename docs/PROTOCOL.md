# 协议与能力边界

以下是适配实现的说明，不是上游官方 SDK 或授权声明。默认上游为 `https://xzfzznt.gaj.sh.gov.cn`，可由受信任管理员更改。协议基于已观察到的公开网页行为，可能随时失效。

## 宿主 ABI

- 动态库导出 `cliproxy_plugin_init`，交换 C ABI 1 函数表。
- 方法通过 JSON envelope 与 RPC schema 6 交互；请求/响应使用明确的缓冲区释放函数。
- executor 绑定 `fanzha` provider 账号池，scope 为 `oauth`，避免绕过宿主凭据选择。
- CPA 传入的标准化 OpenAI 格式可能写为 `openai`；也接受 `chat-completions`。
- 通过 `host.stream.emit` 只交付原始 JSON chunk。SSE 的 `data:` 与末尾 `[DONE]` 由宿主包装；插件先发送 `finish_reason: stop`，再关闭流。

## 登录与刷新

- 邮箱：POST `/api/v1/user/email/login`。
- 手机号：POST `/api/v1/user/login`。
- 密码编码：UTF-8 文本 SHA-256 的小写十六进制，不是 AES，也不是独立认证机制。
- 刷新：POST `/api/v1/user/token/refresh`，Bearer 使用 refresh token，不额外传 body 字段。
- JWT payload 只用于刷新调度提示，不当作本地签名验证或账号授权依据。
- 有明确到期时间且有效期超过 10 分钟时不反复轮换令牌；缺失或临近到期才尝试上游刷新。

## 聊天

1. POST `/api/ai/create_session`，body `{}`，读取嵌套 session ID。
2. POST `/api/ai/chat?type=0`，携带 `conversation_id`、`using_context: false` 及文本。
3. `answer_mode` 对应 `normal`、`detailed`、`english`、`english_detailed`。
4. 解析 SSE `data` 内的回答/思考增量；`exec_elapsed_secs` 可能是数值或字符串。
5. 上游截断、无完成标记、业务拒绝不能当作成功；没有真实 token 用量则省略 usage。

历史消息拼为带角色标记的普通文本，不宣称上游原生支持多角色或持久会话。每次请求新建独立会话。最大输出参数限制为 2048，超出或不支持的内容明确报错。

## 额度

GET `/api/v1/conversations/usage`，必须包含有效 `used`、`remaining`、`limit`。标准化为 CPA quota provider 的 summary 与 buckets；只使用上游有效 reset_time，不自行计算“午夜重置”。不支持手动重置。

## 管理资源

公开静态资源位于 `/v0/resource/plugins/fanzha/`：`login`、`quota`、`logo.png`。它们本身不读取账号令牌。

受宿主管理鉴权保护的插件路由：

- GET `/v0/management/fanzha/accounts`：仅返回必要账号索引与展示字段。
- POST `/v0/management/fanzha/quota`：body `{ "auth_index": "..." }`。
- POST `/v0/management/fanzha/login`：已有账号登录，由宿主保存认证信息。

额度 provider 还可由宿主统一配额接口调用；不要把插件资源路由与 CPA 核心管理 API 的版本前缀混为一谈。
