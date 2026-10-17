# 发布前清单

以下是维护者发布检查清单；构建和 CI 不会自行推送、打标签、发布 Release 或部署生产环境。

## 内容审查

- [ ] 确认本项目代码、图标和第三方材料的发布权利；核对 NOTICE、MIT、CLIProxyAPI 与 YAML/Apache 声明。
- [ ] 运行 `python3 scripts/scan-secrets.py`，人工审查所有文件及附件。脚本跳过 Git 内部与构建目录，并非完整历史扫描。
- [ ] 不上传 CPA 配置/认证 JSON、服务账号、SSH 私钥、管理密钥、浏览器缓存、线上日志、旧探测脚本或备份。
- [ ] 如果后来加入 Git 历史，另外检查全部历史；本地准备目录未复制旧 `.git`，不应把原工作目录整体导入。
- [ ] 版本号、CI ABI 测试命令、README 和更新记录一致。

## 质量与附件

- [ ] `bash scripts/test.sh`。
- [ ] `bash scripts/build.sh`。
- [ ] `python3 scripts/check-abi.py dist/fanzha-v0.2.5-rewrite-linux-amd64.so`。
- [ ] 在目标 CPA 的隔离实例检查注册、账号与资源。只有用户同意额度消耗后才做一次真实聊天验证。
- [ ] 说明 Go、GCC、libc、架构及实际验证范围，不把旧版运行记录当作新版验证。
- [ ] Release 二进制单独上传；附 SHA-256、本项目 LICENSE、NOTICE、`licenses/`，不要上传整个 dist 或机器目录。
- [ ] 核对依赖许可证；若变更依赖，随之更新声明，不把整个二进制的第三方内容都声称为自己 MIT 代码。

## 首次创建远端仓库

在 GitHub 创建 `qqlikeyuanshen/cpa-fanzha-ai` 并确认公开范围。只有确认要发布时，才在当前目录初始化 Git、审阅暂存区、提交并推送；不要把账号令牌写进远端 URL 或命令行。使用已配置的 GitHub 凭据管理方式。

仓库已公开；Release 由维护者确认后发布。CI 工作流只有 `contents: read`，构建附件经校验后才作为正式版本上传。默认安装脚本从 Releases 下载成品，不再执行源码构建。
