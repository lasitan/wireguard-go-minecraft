# Lasitan-Cluster

Lasitan-Cluster 是基于 WireGuard 用户态实现的组网控制面与 Agent：隧道走 TCP，并支持 Minecraft 协议伪装。

命令行程序名为 `lasitan-cluster`。

## 用法

```
$ lasitan-cluster lc0
```

前台运行：

```
$ lasitan-cluster -f lc0
```

Master / Agent / 升级：

```
$ sudo lasitan-cluster install master
$ sudo lasitan-cluster install
$ sudo lasitan-cluster update
$ lasitan-cluster --version
```

更多日志：`LOG_LEVEL=debug`。

## 构建

需要最新版 [Go](https://go.dev/) 与 Node.js（Master 面板）：

```
$ bash deploy/scripts/build-master-ui.sh
$ go build -o lasitan-cluster \
    -ldflags "-X golang.zx2c4.com/wireguard/src/core/version.Version=$(cat .github/build/version.txt)" ./src
```

版本号在 `.github/build/version.txt`；未加 `-ldflags` 时显示 `dev`。

## 目录

```
src/
  main*.go     入口与子命令
  core/        mesh 模型、配置、协议、版本
  wireguard/   WireGuard 用户态核心（device / conn / tun / ipc）
  tunnel/      数据面：配置、NAT、端口转发
  agent/       Agent 轮询 / WebSocket
  master/      控制面：store、stats、hub、api、嵌入 UI
  update/      GitHub Releases 自升级
  commands/    keygen、角色锁
  systems/     Linux / Windows 服务与守护进程
  web/         Master 面板（vite，构建到 master/ui/dist）
deploy/
  debian/ docker/ scripts/ examples/
```

## License

    Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
    (and subsequent Lasitan-Cluster modifications)

    Permission is hereby granted, free of charge, to any person obtaining a copy of
    this software and associated documentation files (the "Software"), to deal in
    the Software without restriction, including without limitation the rights to
    use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
    of the Software, and to permit persons to whom the Software is furnished to do
    so, subject to the following conditions:

    The above copyright notice and this permission notice shall be included in all
    copies or substantial portions of the Software.

    THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
    IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
    FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
    AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
    LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
    OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
    SOFTWARE.
