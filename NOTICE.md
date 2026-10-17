# 来源与第三方声明

- 本项目作者：成实外初一杨lingling；目标仓库：https://github.com/qqlikeyuanshen/cpa-fanzha-ai 。
- 本项目是非官方 CPA 原生插件，与国家反诈中心、公安机关、上游网站运营方以及 CPA 官方均无隶属、授权或背书关系。项目名描述适配用途，不代表官方身份。
- Go/cgo C ABI 声明及宿主回调模式参考了 [CLIProxyAPI v8.0.23 原生插件示例](https://github.com/router-for-me/CLIProxyAPI/tree/v8.0.23/examples/plugins)，并依赖该项目的 `sdk/pluginapi`。对应 MIT 声明保留在 `licenses/CLIProxyAPI-MIT.txt`，其中历史年份按上游原文保留。
- YAML 解析依赖 `gopkg.in/yaml.v3 v3.0.1`，由 Go Modules 获取；其 MIT/Apache 说明保留在 `licenses/yaml-v3-LICENSE.txt`，Apache 2.0 全文保留在 `licenses/Apache-2.0.txt`。分发合并二进制时还应保留所使用依赖的适用许可证声明。
- 早期本地版本沿用过其他项目的仓库占位与功能命名。本整理版移除了该占位，不包含其旧二进制；不声称是其官方发布或获得其授权。公开前如发现任何实际借用的受版权保护材料，应先确认授权并补充准确归属。
- 上游登录、聊天、额度协议根据可公开访问的网页行为整理。仓库不分发抓取的上游 JavaScript、第三方页面、旧插件 `.so`、真实账号或协议探测日志。公开接口不等于允许任意用途，使用前须核对上游条款。
- 按维护者要求保留现用版本的 `logo.png`，以保持原有显示效果。该图标不是本项目原创，不纳入本项目 MIT 授权；其相关权利归原权利人所有，公开分发前应确认使用授权。保留图标不表示本项目与上海市公安局或其他官方机构存在任何关系、授权或背书。

本地整理和许可证选择不是对上游服务授权范围的法律保证。开源发布者应自行确认其拥有所发布内容的相应权利。
