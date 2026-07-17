# Changelog

## v0.2.0

- Add TCP `OnOpen` and `OnClose` connection lifecycle hooks.
- Add context-aware UDP listener creation with `udp.ListenContext`.
- Add UDP socket configuration through `udp.ListenOptions` and
  `netx.UDPServerOptions.Socket`.
- Preserve the v0.1 TCP, UDP, lifecycle, and high-level server APIs.
