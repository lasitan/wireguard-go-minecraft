# Go Implementation of [WireGuard](https://www.wireguard.com/)

This is an implementation of WireGuard in Go.

## Usage

Most Linux kernel WireGuard users are used to adding an interface with `ip link add wg0 type wireguard`. With wireguard-go, instead simply run:

```
$ wireguard-go wg0
```

This will create an interface and fork into the background. To remove the interface, use the usual `ip link del wg0`, or if your system does not support removing interfaces directly, you may instead remove the control socket via `rm -f /var/run/wireguard/wg0.sock`, which will result in wireguard-go shutting down.

To run wireguard-go without forking to the background, pass `-f` or `--foreground`:

```
$ wireguard-go -f wg0
```

When an interface is running, you may use [`wg(8)`](https://git.zx2c4.com/wireguard-tools/about/src/man/wg.8) to configure it, as well as the usual `ip(8)` and `ifconfig(8)` commands.

To run with more logging you may set the environment variable `LOG_LEVEL=debug`.

## Platforms

### Linux

This will run on Linux; however you should instead use the kernel module, which is faster and better integrated into the OS. See the [installation page](https://www.wireguard.com/install/) for instructions.

### Windows

This runs on Windows 10+ (amd64 / arm64). The signed Wintun driver is embedded and released next to the executable on first start.

Other platforms (macOS, BSD, Android, iOS) are not supported by this fork.

## Building

This requires an installation of the latest version of [Go](https://go.dev/) and Node.js (for the Master web panel).

```
$ bash deploy/scripts/build-master-ui.sh
$ go build -o wireguard-go \
    -ldflags "-X golang.zx2c4.com/wireguard/src/core/version.Version=$(cat .github/build/version.txt)" ./src
```

The release version lives in `.github/build/version.txt`; bumping it on `main` triggers the release workflow. Builds without `-ldflags` report `dev`.

## Layout

```
src/
  main*.go     entry point and subcommand dispatch
  core/        mesh models, config files, wire protocol, version
  wireguard/   upstream wireguard-go kernel (device, conn, tun, ipc)
  tunnel/      data plane: wg conf, NAT gateway, port forwards
  agent/       agent polling / websocket client
  master/      control plane: store, stats, geoip, hub, api, embedded ui
  update/      self-update from GitHub Releases
  commands/    keygen, role lock
  systems/     linux / windows service, elevation and daemon main
  utils/       small shared helpers
  web/         Master web panel (vite, built into master/ui/dist)
deploy/
  debian/ docker/ scripts/ examples/
```

## License

    Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
    
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
