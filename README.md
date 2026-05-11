# Home

English | [中文](README_CN.md)

Home is a comprehensive credential **management, scheduling, and distribution** service designed for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).

It serves as a centralized hub for managing and scheduling API credentials in large-scale CLIProxyAPI cluster deployments. CLIProxyAPI nodes communicate with Home to retrieve necessary API credentials and report usage statistics.

## Key Features

- **Scalable Node Management**: Supports connecting an unlimited number of CLIProxyAPI nodes for credential distribution, ideal for large-scale deployments.
- **Multi-Provider OAuth2 Support**: Manages OAuth2 credentials for various AI model providers, including OpenAI, Gemini, Claude, Codex, and more.
- **High Availability**: Supports multi-account management and round-robin load balancing to ensure optimal performance and reliability.
- **Secure Access**: Provides a streamlined CLI authentication flow to ensure secure access to APIs.
- **Flexible Upstream Integration**: Easily integrate upstream providers via configuration, including:
  - OpenAI-compatible providers (e.g., OpenRouter)
  - OpenAI Responses protocol providers
  - Anthropic Messages protocol providers
  - Google Gemini protocol providers

## 集群模式

当工作目录中存在 `cluster.yaml` 时，Home 会启用基于 PGSQL 的集群模式；当 `cluster.yaml` 不存在时，Home 会保持单节点模式并继续使用本地配置。

可以复制 `cluster.example.yaml` 为 `cluster.yaml` 后按实际环境修改。PGSQL 连接必须使用 TCP `host` 和 `port`，不能使用 Unix Socket。集群模式启动并完成迁移后，会删除已经导入的本地凭证文件和 `config.yaml`，避免后续继续读取旧的本地状态。

集群模式下，Management API 会直接操作 PGSQL 中的数据。默认监听端口来自 `cluster.yaml` 的 `node.port`；也可以通过启动参数 `-addr` 覆盖监听地址，但实际集群节点端口始终以最终监听端口为准。

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository.
2. Create your feature branch (`git checkout -b feature/amazing-feature`).
3. Commit your changes (`git commit -m 'Add some amazing feature'`).
4. Push to the branch (`git push origin feature/amazing-feature`).
5. Open a Pull Request.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
