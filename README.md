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

## Cluster Mode

Home enables PGSQL-backed cluster mode when `cluster.yaml` exists in the working directory. When `cluster.yaml` is absent, Home keeps the single-node mode and continues using local configuration.

Copy `cluster.example.yaml` to `cluster.yaml`, then adjust it for the target environment. PGSQL connections must use a TCP `host` and `port`; Unix sockets are not supported. After cluster mode starts and completes migration, Home deletes the imported local credential files and `config.yaml` so it will not keep reading stale local state.

In cluster mode, the Management API operates directly on data stored in PGSQL. The default listen port comes from `node.port` in `cluster.yaml`; the startup `-addr` flag can override the listen address, but the effective cluster node port always follows the final listen port.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository.
2. Create your feature branch (`git checkout -b feature/amazing-feature`).
3. Commit your changes (`git commit -m 'Add some amazing feature'`).
4. Push to the branch (`git push origin feature/amazing-feature`).
5. Open a Pull Request.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
