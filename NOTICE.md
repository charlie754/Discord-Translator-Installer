# Notices

## Derivative Work

Discord Translator Installer is a derivative of [Equilotl](https://github.com/Equicord/Equilotl), the
installer for [Equicord](https://github.com/Equicord/Equicord), which is itself a fork of
[Vencord](https://github.com/Vendicated/Vencord) by Vendicated and contributors.

Equilotl is licensed under GPL-3.0, as is this installer.

### Provenance

Created by importing Equilotl at commit `c6bfed9` on August 18, 2026.

### Modifications

The following modifications were made on August 18 and 19, 2026:

- Rebranded the installer's identity, window title, environment variables and data directory
  from Equicord to Discord Translator
- Repointed all four release URLs at this project, including the two fallbacks that fire only
  on a GitHub rate limit and would otherwise have installed upstream Equicord
- Added build and release workflows for Windows, macOS and Linux
- Repaired Go identifiers broken by the rebrand, and two pre-existing `go vet` defects
  inherited from upstream: an `fmt.Errorf` call with no format directives that silently
  discarded both its arguments, and a non-constant format string
- Corrected the environment variable names so they match the ones the client actually reads

The following further modifications were made on August 20, 2026:

- Replaced the application icon with this project's own artwork, and added the CI step that
  embeds it — nothing had ever run `go-winres`, so every previous binary shipped with Go's
  default icon regardless of what `winres/` contained
- Removed the anti-phishing card's reference to a domain that does not exist
- Removed the install-path display and the environment-variable instruction from the main screen
- **Removed OpenAsar support entirely**, including `openasar.go`, which downloaded an executable
  bundle from a third party and wrote it over a core part of the user's Discord. The GUI button,
  both CLI flags, the interactive menu entries and the detection cache were all removed with it.
- Stamped the version and commit hash into the binary at build time. Without this the installer
  reported its version as `Unknown` and, because the self-update check compares against that
  value, believed itself out of date on every run
- Rewrote the README, whose links had been corrupted into unusable URLs by the original rebrand
  and which embedded a third-party screenshot of upstream's installer

The following further modifications were made on August 22, 2026:

- Fixed the self-updater, which destroyed the installer it was meant to update. The rebrand
  had find-replaced the display name into the GitHub org, the repository and every asset
  filename, so every download URL 404ed; `UpdateSelf` never checked the HTTP status, so the
  404 body was written over the running executable, which was then reported as a successful
  update. Corrected every URL, and added a status check and a size floor before the
  destructive write
- Corrected the Linux download, which served the command-line binary to graphical installs

### Source Attribution

All upstream copyright notices are preserved in the source code.

Complete corresponding source is available at
https://github.com/charlie754/Discord-Translator-Installer, satisfying GPL-3.0 section 6(d).
