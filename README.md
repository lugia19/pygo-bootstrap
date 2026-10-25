# pygo-bootstrap

A small **Go + Python bootstrapper/updater**. It pulls an application from a GitHub repo, sets up an
isolated Python environment for it, keeps it up to date on each launch, and starts it — wrapped in a
native launcher: a `.exe` on Windows and a signed **universal `.app`** on macOS.

It's meant to be used as a template: you point it at your repo via `repo.json`, drop in an icon, and
build. The end user just runs the launcher.

## How it works

**Go launcher** (the entry point):
- Uses [`uv`](https://github.com/astral-sh/uv) to download a Python runtime and create a venv (once).
- Installs the base GUI packages (PyQt6, dulwich, requests, googletrans) into that venv.
- Runs `install.py` with the venv's Python.

**`install.py`**:
- Clones (or pulls) the configured GitHub repo with dulwich.
- Installs the repo's `requirements.txt` via uv, updating them when the repo changes.
  - If a `requirements-torch.txt` is present it's installed first, with a GUI download-progress dialog
    for large wheels — handy for CUDA PyTorch.
- Launches the repo's startup script to start the app.

## Quick start (building)

Builds run **on Windows** and cross-compile every target, including the macOS `.app`.

Prerequisites: **Go**, and **WSL with `zip`** (only needed to package the macOS bundle).

1. Copy `repo-example.json` → `repo.json` and fill in your details.
2. Put a square icon (256×256 or larger) at `resources\icon.png`.
3. Set `$APP_NAME` / `$BUNDLE_ID` / `$VERSION` at the top of `build-all.ps1`, then run it:
   ```powershell
   .\build-all.ps1
   ```

Outputs land in `builds\`:
- `…-windows.zip` — `<AppName>.exe` + `installer-resources\`
- `…-macos-universal.zip` — the signed universal `.app`

Use `-WindowsOnly` or `-MacOnly` to build a single platform.

> Keep `$APP_NAME` stable across releases — on macOS it also names the per-user data directory.

The first build downloads its tools (`uv` for each platform, `rcedit`, `rcodesign`, `konoui/lipo`)
into a gitignored `resources\` cache and reuses them afterward. It then converts `icon.png` to
`app.ico` (embedded in the `.exe`) and `app.icns` (in the `.app`), builds and packages the Windows
launcher, and builds both macOS arches — fusing them and `uv` into universal binaries with `lipo`,
assembling the bundle, and ad-hoc-signing it with `rcodesign`.

## `repo.json` fields

| Field | Description |
|---|---|
| `repo_url` | GitHub repository URL to clone/pull |
| `repo_dir` | Local directory name for the cloned repo |
| `startup_script` | Python script to run from the repo (e.g. `main.py`) |
| `use_pythonw` | Windows: use `pythonw.exe` (no console) instead of `python.exe`. Ignored on macOS. |
| `venv_folder` | Virtual environment folder name (default: `venv`) |
| `python_version` | Python version uv installs (e.g. `3.11`) |
| `icon` | Icon filename — use a cross-platform **`.png`**; the build converts it to `.ico`/`.icns` |
| `install_per_user` | Optional, default `false` (portable). `true`: the launcher installs itself into a known per-user folder and runs from there — see [Runtime layout](#runtime-layout) |

## Runtime layout

The launcher reads shipped inputs from a read-only **resource dir** and writes everything it generates
to a writable **data dir**:

| | Resource dir (`uv`, `install.py`, `repo.json`, `icon.png`) | Data dir (Python runtime, venv, `logs/`, cloned repo) |
|---|---|---|
| **Windows** | `installer-resources\` beside the `.exe` | beside the `.exe` (a self-contained, portable folder) |
| **macOS** | `<AppName>.app/Contents/Resources/` | `~/Library/Application Support/<AppName>/` |

Splitting the two lets the macOS `.app` stay read-only (e.g. in `/Applications`). A shipped Windows
folder looks like:

```
<AppName>\
  <AppName>.exe
  installer-resources\          (repo.json, install.py, uv.exe, icon.png)
  venv\  python\  logs\  <repo>\   (created on first run)
```

### Per-user install (`install_per_user: true`)

An optional mode for apps that should live in one known place rather than wherever the download
was unzipped:

| | Data dir |
|---|---|
| **Windows** | `%LOCALAPPDATA%\Programs\<AppName>\` |
| **macOS** | `~/Library/Application Support/<AppName>/install/` |

On Windows, a launcher started from anywhere else (the unzipped download) copies itself and its
`installer-resources\` into that folder, adds a Start Menu entry, starts the installed copy and
exits; the download folder is never written to. Running a newer download installs it over the old
one (a copy still running is renamed aside to `.old` and cleared on the next install). `<AppName>`
is `$APP_NAME`, stamped into the exe at build time, so a renamed download installs to the same place.
Uninstalling is deleting that folder and the Start Menu entry.

On macOS the `.app` is already the install; the mode moves the launcher's files down into
`install/`, leaving `Application Support/<AppName>/` to the app's own data.

Either way, keep the app's own data (settings, documents) out of the data dir: put it in the
user's data folder (`%APPDATA%\<AppName>`, `~/Library/Application Support/<AppName>`), so
reinstalling or deleting the install never touches it.

## macOS distribution note

The `.app` is **ad-hoc** signed (no Apple Developer account required), so Gatekeeper still warns on
first launch. Users right-click → **Open**, or clear the quarantine flag:

```bash
xattr -dr com.apple.quarantine /path/to/<AppName>.app
```
