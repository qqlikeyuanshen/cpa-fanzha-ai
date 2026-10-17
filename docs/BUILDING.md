# 构建与测试

## 环境

- Linux amd64，Go 1.26.0，GCC/系统 C 开发文件。
- Node.js 22：检查内嵌页面脚本语法，无 npm 依赖。
- Python 3：标准库即可执行敏感信息扫描和 ctypes C ABI 测试。

在 Debian/Ubuntu 中可按发行版安装 `build-essential`、`python3`，另安装匹配的 Go 与 Node.js。不要直接使用发行版中过旧的 Go。首次构建需要访问公开 Go 模块代理及校验服务；受限网络下可自行选择可信 GOPROXY，但不要关闭 TLS 验证或校验数据库来掩盖下载失败。

## 常用命令

```bash
go version
gcc --version
bash scripts/test.sh
bash scripts/build.sh
python3 scripts/check-abi.py dist/fanzha-v0.2.5-rewrite-linux-amd64.so
```

底层构建命令：

```bash
CGO_ENABLED=1 go build -mod=readonly -trimpath -buildmode=c-shared \
  -o dist/fanzha-v0.2.5-rewrite-linux-amd64.so .
```

`go.mod` 固定公开 CPA SDK `v8.0.23`，无本地 `replace`。直接升级宿主并不要求盲目升级 SDK；先核对 C ABI、RPC schema 和字段，再变更依赖并跑回归。`go.sum` 应提交到 Git。

## 检查覆盖面

- 账号解析保留 ID、元数据，忽略其他 provider。
- 登录 SHA-256 编码、令牌刷新 Bearer 协议、未到期令牌不反复轮换、保存数据不含密码。
- 额度字段必须真实完整，缺失时报错，不虚构重置时间。
- 四种模型、provider 账号池路由、纯文本输入边界。
- SSE CRLF/多行、思考增量、字符串耗时、截断与业务错误；宿主流回调仅发送裸 JSON，不重复包装 `data:`。
- 公开静态资源不访问宿主账号；元数据与原版 PNG 图标检查。
- `check-abi.py` 在独立进程加载构建的 `.so`，验证真实 C ABI、RPC、资源、错误和 shutdown；只用 mock 凭据，不连接上游或运行中的 CPA。

模拟测试不是上游可用性保证。`scan-secrets.py` 仅是启发式当前工作树检查，排除 `.git` 和构建目录，不替代人工检查或历史扫描。

## 二进制可移植性

CGO 构建依赖目标 libc 与架构。建议在不比部署目标更新的 Linux 用户空间编译；例如在新 Debian 编译的动态库不保证能在旧 Ubuntu 加载。CI 使用 Ubuntu 22.04 amd64，但实际执行前仍须检查目标环境。可以用 `file`、`ldd`、`readelf --version-info` 检查产物。不要跨架构复制 `.so`。

arm64 路径为预留能力，尚未在本整理版验证；请在原生 Linux arm64 上构建测试。跨编译还需要正确的目标 C 工具链，单设 `GOARCH` 不够。

## 无凭据的构建原则

构建、单元测试、ABI 测试均不需要生产配置、API Key 或真实账号。不要把认证目录挂入构建容器。`dist/` 不进入源码 Git；发布附件时另行审阅并附适用许可证与校验值。不要承诺逐字节可重现：Go、GCC、libc 与系统版本都可能影响结果。
