# Installing OSIRIS JSON Producers<!-- omit in toc -->

OSIRIS JSON Producers discover infrastructure resources and export
vendor-neutral OSIRIS JSON snapshots documents. Pre-built distributions
are provided for Linux, macOS and Windows, so **Go is not required** for
a normal installation.

**Useful links:** [Latest releases](https://github.com/osirisjson/osiris-producers/releases), [Installation guide](https://docs.osirisjson.org/osiris-producers/installation/), [Getting started](https://docs.osirisjson.org/osiris-producers/getting-started/)

## Table of content<!-- omit in toc -->
- [1. Choose an installation method](#1-choose-an-installation-method)
- [2. Install using a package manager](#2-install-using-a-package-manager)
  - [Linux .deb](#linux-deb)
  - [Linux .rpm](#linux-rpm)
  - [Apple macOS Homebrew](#apple-macos-homebrew)
  - [Microsoft Windows WinGet (PowerShell)](#microsoft-windows-winget-powershell)
  - [Microsoft Windows Scoop (PowerShell)](#microsoft-windows-scoop-powershell)
- [3. Install manually from a release archive](#3-install-manually-from-a-release-archive)
  - [3.1 Select the correct archive](#31-select-the-correct-archive)
  - [3.2 Verify the downloaded archive before installation](#32-verify-the-downloaded-archive-before-installation)
  - [3.3 Linux extract and install](#33-linux-extract-and-install)
  - [3.4 Apple macOS extract and install](#34-apple-macos-extract-and-install)
  - [3.5 Microsoft Windows extract and add to PATH](#35-microsoft-windows-extract-and-add-to-path)
- [4. Install using Go](#4-install-using-go)
- [5. Verify the installation and get started](#5-verify-the-installation-and-get-started)
- [6. Updating an existing OSIRIS JSON Producer installation](#6-updating-an-existing-osiris-json-producer-installation)
- [7. Troubleshooting and security prompts](#7-troubleshooting-and-security-prompts)
  - [Permission denied or wrong architecture](#permission-denied-or-wrong-architecture)
  - [Apple macOS security prompts](#apple-macos-security-prompts)
  - [Microsoft Windows SmartScreen prompts](#microsoft-windows-smartscreen-prompts)
  - [Package not found in Microsoft WinGet Scoop or Homebrew](#package-not-found-in-microsoft-winget-scoop-or-homebrew)
- [8. Documentation and support](#8-documentation-and-support)


## 1. Choose an installation method

| Platform | Recommended method | Alternative |
|---|---|---|
| **Debian-based Linux** (Debian, Ubuntu, Linux Mint, Kali Linux, Parrot OS, Raspberry Pi OS, Zorin OS) | Release `.deb` | Release `.tar.gz` |
| **RPM-based Linux** (RHEL, Fedora, CentOS, Rocky Linux, AlmaLinux, openSUSE, SLES, Oracle Linux, Amazon Linux) | Release `.rpm` | Release `.tar.gz` |
| **Other supported Linux distributions** | Release `.tar.gz` | Build/install with Go |
| **macOS** (Apple Silicon / Intel) | Homebrew | Release `.tar.gz` |
| **Windows** (x86-64) | WinGet or Scoop | Release `.zip` |

## 2. Install using a package manager

### Linux .deb

Download the matching `.deb` file from [GitHub Releases](https://github.com/osirisjson/osiris-producers/releases) and run the following from its
download directory.

```bash
# Install or upgrade
sudo apt install ./osiris-producers_<version>_linux_amd64.deb

# Verify
osirisjson-producer --version
```

The example is for x86-64 (`amd64`). For other architectures, use a
matching release package only if it is published.

### Linux .rpm

Download the matching `.rpm` from [GitHub Releases](https://github.com/osirisjson/osiris-producers/releases), then:

```bash
# Install or upgrade RHEL / Fedora and DNF-based distributions
sudo dnf install ./osiris-producers_<version>_linux_amd64.rpm

# Install or upgrade openSUSE / SUSE Linux Enterprise
sudo zypper install ./osiris-producers_<version>_linux_amd64.rpm

# Alternative RPM directly
sudo rpm -Uvh ./osiris-producers_<version>_linux_amd64.rpm

# Verify
osirisjson-producer --version
```

The example is for x86-64 (`amd64`). For other architectures, use a
matching release package only if it is published.

### Apple macOS Homebrew

**Prerequisite:** Homebrew must be installed and available in your
terminal. If you haven't installed it yet, follow the official
[Homebrew installation guide](https://brew.sh/).
After installation, follow the instructions displayed by Homebrew to
configure your `PATH`, if required.

```bash
# Verify Homebrew is available
brew --version

# Install OSIRIS JSON Producers
brew tap osirisjson/osiris-producers
brew install osirisjson-producer

# Verify installation
osirisjson-producer --version
```

### Microsoft Windows WinGet (PowerShell)

**Prerequisite:** WinGet is included with Microsoft **App Installer** on
supported Windows versions. If `winget` is not recognized, install or
update App Installer from the Microsoft Store. See the official
[Microsoft WinGet installation guide](https://learn.microsoft.com/en-us/windows/package-manager/winget/).

```powershell
# Verify WinGet is available
winget --version

# Install OSIRIS JSON Producers
winget install --id osirisjson.osirisjson-producer --exact

# Verify installation
osirisjson-producer --version
```

### Microsoft Windows Scoop (PowerShell)

**Prerequisite:** Scoop must be installed before using this method.
If it is not available, follow the official [Scoop installation instructions](https://scoop.sh/). Scoop is typically installed in a regular, non-administrator PowerShell
session and may require configuring the current user's PowerShell
execution policy.

```powershell
# Verify Scoop is available
scoop --version

# Add the OSIRIS Producers bucket
scoop bucket add osiris-producers https://github.com/osirisjson/scoop-osiris-producers

# Install OSIRIS JSON Producers
scoop install osirisjson-producer

# Verify installation
osirisjson-producer --version
```


## 3. Install manually from a release archive

Manual installation does not require Homebrew, WinGet, Scoop or Go.

### 3.1 Select the correct archive

On the [Releases page](https://github.com/osirisjson/osiris-producers/releases)
choose the archive that matches your operating system and CPU
architecture:

| Operating system | Architecture | Asset filename pattern |
| --- | --- | --- |
| Linux | x86-64 | `osiris-producers_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `osiris-producers_<version>_linux_arm64.tar.gz` |
| macOS | Intel (x86-64) | `osiris-producers_<version>_darwin_amd64.tar.gz` |
| macOS | Apple Silicon (ARM64) | `osiris-producers_<version>_darwin_arm64.tar.gz` |
| Windows | x86-64 | `osiris-producers_<version>_windows_amd64.zip` |

Check your architecture with `uname -m` on Linux/macOS (`x86_64` means
`amd64`; `aarch64`/`arm64` means `arm64`). On Windows, open
**Settings -> System -> About -> System type**.

### 3.2 Verify the downloaded archive before installation

Download the release's `osiris-producers_<version>_checksums.txt`
alongside the archive.
Compare the result with the matching entry in `checksums.txt`.
**Do not install or run a downloaded file if its checksum does not match.**
Checksums detect changes relative to the published manifest; they do not
independently establish publisher identity.

**Linux:**

```bash
sha256sum --check --ignore-missing "osiris-producers_${VERSION}_checksums.txt"
```

**macOS:** (`shasum` is included with macOS)

```bash
shasum -a 256 "osiris-producers_${VERSION}_darwin_arm64.tar.gz"
```

**Windows (PowerShell):**

```powershell
(Get-FileHash ".\osiris-producers_${Version}_windows_amd64.zip" -Algorithm SHA256).Hash
```


### 3.3 Linux extract and install

For x86-64:

```bash
tar -xzf "osiris-producers_${VERSION}_linux_amd64.tar.gz"
sudo install -m 0755 osirisjson-producer /usr/local/bin/osirisjson-producer
osirisjson-producer --version
```

For ARM64, use `linux_arm64` in the archive filename. If the archive
contains a subdirectory, use the actual extracted path to
`osirisjson-producer` in the `install` command.

### 3.4 Apple macOS extract and install

For Apple Silicon:

```bash
tar -xzf "osiris-producers_${VERSION}_darwin_arm64.tar.gz"
sudo install -m 0755 osirisjson-producer /usr/local/bin/osirisjson-producer
osirisjson-producer --version
```

For Intel Macs, use `darwin_amd64` in the archive filename.
If the archive contains a subdirectory, use the actual extracted binary
path. See [macOS security prompts](#macos-security-prompts) if macOS
blocks a manually installed executable.

### 3.5 Microsoft Windows extract and add to PATH

1. Download and verify the Windows `.zip` archive.
2. Extract the archive. Copy `osirisjson-producer.exe` to `%LOCALAPPDATA%\Programs\osirisjson\` (create the directory if needed).
3. In **PowerShell**, add that directory to your **user** PATH without
   overwriting the system PATH:

```powershell
$Bin = Join-Path $env:LOCALAPPDATA 'Programs\osirisjson'
$UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$UserEntries = @($UserPath -split ';' | Where-Object { $_ })
if ($UserEntries -notcontains $Bin) {
    [Environment]::SetEnvironmentVariable('Path', (($UserEntries + $Bin) -join ';'), 'User')
}
$env:Path += ";$Bin"  # Make the executable available in this PowerShell session
osirisjson-producer --version
```

Open a new terminal for the permanent PATH change to take effect.
This example assumes the executable is directly inside the directory,
not a nested extraction folder.

## 4. Install using Go

If you already have **Go 1.22 or later** you can install the packages
from the Go module:

```bash
go install go.osirisjson.org/producers/cmd/...@latest
```

This installs the command packages provided by the selected module
version. The binaries are placed in `GOBIN`, or in `$(go env GOPATH)/bin`
if `GOBIN` is not set. Add that directory to your PATH if necessary.
To reproduce an installation, replace `@latest` with a specific
published module version.

The Go toolchain is **not** required for any of the pre-built release
methods above.

## 5. Verify the installation and get started

From your terminal run:

```bash
osirisjson-producer --version
osirisjson-producer --help
```

The first command should display the installed producer release version,
the second should display the supported command interface.
For OSIRIS JSON producer-specific usage see [Getting started](https://docs.osirisjson.org/osiris-producers/getting-started/).

> [!NOTE]
> **OSIRIS JSON specification format vs. OSIRIS JSON Producer version:**
> The installed executable's version is not the OSIRIS JSON document
> specification version. The latter is defined separately by the [OSIRIS JSON v1.0 specification](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md) and [schema](https://osirisjson.org/schema/v1.0/osiris.schema.json).

## 6. Updating an existing OSIRIS JSON Producer installation

Use the same installation method you originally selected:

| Installation method | Typical update command or action |
| --- | --- |
| Homebrew | `brew update && brew upgrade osirisjson-producer` |
| WinGet | `winget upgrade --id osirisjson.osirisjson-producer --exact` |
| Scoop | `scoop update osirisjson-producer` |
| Manual archive | Download, verify and replace with the newer release binary |
| `.deb` / `.rpm` | Download and install the newer package using your distribution's upgrade procedure |
| Go | Re-run `go install` with a chosen module version |

After updating, run `osirisjson-producer --version` again.

## 7. Troubleshooting and security prompts

```bash
osirisjson-producer: command not found or not recognized
```

Check that the executable is in a directory on PATH. Reopen the terminal
after changing PATH. For Go installs, check `go env GOPATH` and
`go env GOBIN`. On Linux/macOS, use `command -v osirisjson-producer`, on
Windows, use `Get-Command osirisjson-producer` in PowerShell.

### Permission denied or wrong architecture

Ensure the binary is executable and that the downloaded archive matches
the operating system and CPU architecture. On managed machines, use an
approved user-writable location rather than changing system-wide
installation permissions.

### Apple macOS security prompts

A release installed manually from a `.tar.gz` may prompt macOS
Gatekeeper if the executable is not notarized. Verify the download's
source and checksum first. If you trust the release, use the
macOS-provided **Open** / **Open Anyway** workflow for that specific
application. Do not disable Gatekeeper system-wide.

If required by your organization's policies, an administrator may
instead authorize the specific verified executable. Homebrew is
generally the simpler installation path.

### Microsoft Windows SmartScreen prompts

Windows may warn about an unfamiliar or unsigned application.
Verify the download and publisher first do not bypass a warning for an
unexpected file. If your security policy permits it and you trust the
verified release, follow Windows application-specific
**More info -> Run anyway** flow, or use the file's
**Properties -> Unblock** option when available.
Do not disable SmartScreen globally.

### Package not found in Microsoft WinGet Scoop or Homebrew

Confirm the package name and configured repository or bucket.
Package availability can lag behind a GitHub release. Use the matching,
checksum-verified archive if the package is not currently available.

## 8. Documentation and support

- [Online installation guide](https://docs.osirisjson.org/osiris-producers/installation/)
- [OSIRIS JSON Producer getting started](https://docs.osirisjson.org/osiris-producers/getting-started/)
- [Releases and checksums](https://github.com/osirisjson/osiris-producers/releases)
- [Open Issues and support](https://github.com/osirisjson/osiris-producers/issues)