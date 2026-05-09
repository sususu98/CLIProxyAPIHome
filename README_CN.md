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

## 贡献

欢迎贡献！请随时提交 Pull Request。

1. Fork 仓库
2. 创建您的功能分支（`git checkout -b feature/amazing-feature`）
3. 提交您的更改（`git commit -m 'Add some amazing feature'`）
4. 推送到分支（`git push origin feature/amazing-feature`）
5. 打开 Pull Request

## 许可证

此项目根据 MIT 许可证授权 - 有关详细信息，请参阅 [LICENSE](LICENSE) 文件。