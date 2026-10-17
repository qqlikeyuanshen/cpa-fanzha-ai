# CPA 反诈 AI 插件

作者：成实外初一杨lingling

## 免责声明

本项目是由个人开发、维护的第三方开源适配工具，与上海市公安局及其下属单位不存在任何隶属、合作、委托、授权或背书关系，也不代表上海市公安局的立场、意见或官方服务。本项目不是上海市公安局发布的官方软件，项目名称、页面及相关标识仅用于说明适配对象，不应被理解为获得官方认证或认可。

本项目仅供合法的技术学习、研究与个人使用。使用者应遵守相关法律法规及上游平台的服务条款，仅使用本人拥有或已获合法授权的账号，不得用于诈骗、冒充官方、侵犯他人权益、批量滥用或其他违法违规活动。账号、密码、令牌及提交内容的安全由使用者妥善管理，请勿向不可信的服务提交个人隐私或其他敏感信息。

本项目按现状提供，不保证上游服务持续可用，也不对接口变更、服务中断、账号限制、数据丢失或输出内容的准确性作出承诺。模型生成内容不代表上海市公安局或项目维护者的观点，不应替代警方正式答复、法律意见或专业判断；涉及报警、求助或案件咨询，请通过官方渠道办理。本声明不排除或限制依法不得免除的责任。

## 安装

直接安装 [Releases](https://github.com/qqlikeyuanshen/cpa-fanzha-ai/releases/latest) 编译成品，无需 Git、Go 或 GCC。适用于 Linux amd64、glibc 2.35 及以上；把下面的路径换成自己的 CPA 插件扫描目录，已有旧版先备份并移出。

```bash
curl -fL https://github.com/qqlikeyuanshen/cpa-fanzha-ai/releases/latest/download/install.sh -o install-fanzha.sh &&
bash install-fanzha.sh /opt/cliproxyapi/plugins/linux/amd64
```

脚本下载成品并校验 SHA-256，不修改 CPA 配置或自动重启服务；安装后在 CPA 中启用反诈插件并重新加载。

## 构建的方法

Linux 环境安装 Git、Go 1.26.0 和 GCC 后执行，产物位于 `dist/`。

```bash
git clone https://github.com/qqlikeyuanshen/cpa-fanzha-ai.git &&
cd cpa-fanzha-ai &&
bash scripts/build.sh
```
