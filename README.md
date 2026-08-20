# Discord Translator Installer

Installs [Discord Translator](https://github.com/charlie754/Discord-Translator-Client) into your
Discord desktop app. It downloads `desktop.asar` from the latest release and patches your existing
Discord — you do not install a second app.

**Close Discord completely before running it**, including the tray icon. The installer cannot replace
a file Discord is holding open.

![Discord Translator translating a Japanese channel](assets/preview.gif)

*What you get once it is installed. 47 seconds at 33fps and 86 MB, so give it a moment to load.*

## Download

Everything below comes from the [latest release](https://github.com/charlie754/Discord-Translator-Installer/releases/latest).
`DiscordTranslatorInstaller*` are graphical; `DiscordTranslatorInstallerCli*` run in a terminal.

### Windows

- [Graphical](https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/DiscordTranslatorInstaller.exe)
- [Command line](https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/DiscordTranslatorInstallerCli.exe)

### macOS

- [Graphical, Apple Silicon](https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/DiscordTranslatorInstaller-darwin-arm64)
- [Command line, Apple Silicon](https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/DiscordTranslatorInstallerCli-darwin-arm64)
- [Command line, Intel](https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/DiscordTranslatorInstallerCli-darwin-amd64)

**Intel Macs have no graphical build yet** — only the command-line one. The graphical build links a C
UI toolkit, so it cannot be cross-compiled, and it is produced on an Apple Silicon runner.

### Linux

- [Graphical](https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/DiscordTranslatorInstaller-linux)
- [Command line](https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/DiscordTranslatorInstallerCli-linux)

## Before you run it

These binaries are **not code-signed**, so:

- **Windows** shows a SmartScreen warning. More info → Run anyway.
- **macOS** refuses a double-click. Right-click → Open, then confirm.
- **macOS and Linux** command-line builds need the executable bit: `chmod +x <file>`.

**GitHub is the only official place to get this.** Any other site claiming to be us is not. If you
downloaded it from somewhere else, delete it, run a malware scan, and change your Discord password.

## Platform support, honestly

| | |
|---|---|
| Windows | tested |
| macOS, Apple Silicon | builds, not yet run by anyone |
| macOS, Intel | command line only |
| Linux | builds, not yet run by anyone |

Client modifications are against Discord's Terms of Service. There are no known cases of bans for
using client mods, but the risk is not zero and it is your account at stake.

## Building from source

You need [Go](https://go.dev/doc/install) and a C compiler — MinGW on Windows — because the graphical
build links a C UI toolkit. The command-line build is pure Go and needs neither.

<details>
<summary>Linux also needs some development headers</summary>

#### Base

```sh
apt install -y pkg-config libsdl2-dev libglx-dev libgl1-mesa-dev
dnf install pkg-config libGL-devel libXxf86vm-devel
```

#### X11

```sh
apt install -y xorg-dev
dnf install libXcursor-devel libXi-devel libXinerama-devel libXrandr-devel
```

#### Wayland

```sh
apt install -y libwayland-dev libxkbcommon-dev wayland-protocols extra-cmake-modules
dnf install wayland-devel libxkbcommon-devel wayland-protocols-devel extra-cmake-modules
```

</details>

```sh
go mod tidy

go build                    # graphical
go build --tags wayland     # graphical, Linux Wayland
go build --tags cli         # command line
```

A plain `go build` produces a working binary, but it reports its version as `Unknown` and therefore
always believes it is out of date. The release build stamps the version in:

```sh
go build -ldflags "-X discordtranslator/buildinfo.InstallerTag=v0.0.0-dev -X discordtranslator/buildinfo.InstallerGitHash=$(git rev-parse HEAD)"
```

See [the release workflow](https://github.com/charlie754/Discord-Translator-Installer/blob/main/.github/workflows/release.yml)
for exactly what CI does.

## Licence

GPL-3.0, derived from Equilotl. See [NOTICE.md](NOTICE.md).
