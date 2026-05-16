# Home

[English](README.md) | 中文

Home 是一个为 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 设计的全面凭证**管理、调度和分发**服务。

它作为在大规模 CLIProxyAPI 集群部署中管理和调度 API 凭证的中心枢纽。CLIProxyAPI 节点与 Home 通信以获取必要的 API 凭证并上报使用统计数据。

## 核心特性

- **可扩展的节点管理**：支持连接无限数量的 CLIProxyAPI 节点进行凭证分发，非常适合大规模部署。
- **多提供商 OAuth2 支持**：管理包括 OpenAI、Gemini、Claude、Codex 等在内的各种 AI 模型提供商的 OAuth2 凭证。
- **高可用性**：支持多账号管理和轮询负载均衡，以确保最佳性能和可靠性。
- **安全访问**：提供精简的 CLI 身份验证流程，确保安全访问 API。
- **灵活的上游集成**：通过配置轻松集成上游提供商，包括：
  - OpenAI 兼容提供商（例如 OpenRouter）
  - OpenAI Responses 协议提供商
  - Anthropic Messages 协议提供商
  - Google Gemini 协议提供商

## 集群模式

当工作目录中存在 `cluster.yaml` 时，Home 会启用基于 PGSQL 的集群模式；当 `cluster.yaml` 不存在时，Home 会保持单节点模式并继续使用本地配置。

可以复制 `cluster.example.yaml` 为 `cluster.yaml` 后按实际环境修改。PGSQL 连接必须使用 TCP `host` 和 `port`，不能使用 Unix Socket。集群模式启动并完成迁移后，会删除已经导入的本地凭证文件和 `config.yaml`，避免后续继续读取旧的本地状态。

集群模式下，Management API 会直接操作 PGSQL 中的数据。默认监听端口来自 `cluster.yaml` 的 `node.port`；也可以通过启动参数 `-addr` 覆盖监听地址。如果前置反代改变了 client 实际需要连接的端口，可以设置 `node.external-port`；未设置时，对外发布的集群节点端口仍以最终监听端口为准。

## 贡献

欢迎贡献！请随时提交 Pull Request。

1. Fork 仓库
2. 创建您的功能分支（`git checkout -b feature/amazing-feature`）
3. 提交您的更改（`git commit -m 'Add some amazing feature'`）
4. 推送到分支（`git push origin feature/amazing-feature`）
5. 打开 Pull Request

## 许可证

此项目根据 MIT 许可证授权 - 有关详细信息，请参阅 [LICENSE](LICENSE) 文件。
