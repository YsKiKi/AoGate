# AoGate - Generic Gatekeeper

AoGate 是一个通用的 TCP 服务安全外壳（代理认证网关）。它通过密钥对（支持 Ed25519, RSA, ECDSA）进行身份验证。
只有持有正确私钥并通过 Launcher 验证的客户端，其 IP 地址才会被允许访问后端目标服务器。

## 功能特性

*   **安全防护**：保护 TCP 服务（如游戏服务器、数据库等），防止未经授权的访问。
*   **多密钥支持**：支持 Ed25519 (默认), RSA (2048), ECDSA (P-256) 算法。
*   **组件分离**：包含 Server, Launcher, Keygen, Packager 四个独立组件，各司其职。
*   **配置灵活**：支持命令行参数覆盖配置文件，支持自动生成默认配置。
*   **便捷分发**：内置打包工具，可一键生成包含客户端和私钥的 ZIP 包。

## 使用说明

具体使用说明请点击 [这里](/Usage.md)

## License

[GPL-3.0 license](/LICENSE)