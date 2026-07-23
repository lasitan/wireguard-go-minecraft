# Wintun (Windows TUN driver DLL)

Official signed `wintun.dll` from [wintun.net](https://www.wintun.net/).

- Version: **0.14.1**
- Arch embedded here: **amd64** (`amd64/wintun.dll`)
- Zip SHA-256: `07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51`

The Windows build embeds this DLL and releases it next to the `.exe` at runtime
(so users do not need to copy `wintun.dll` manually).

Refresh with:

```bash
./scripts/fetch-wintun.sh
```

See `LICENSE.txt` in this directory (from the official zip) for redistribution terms.
