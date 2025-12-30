# igcapt

Small Windows tray utility that repurposes **Caps Lock** to switch input language/layout.

## What it does

- **Caps Lock** (without Shift) → switches the current keyboard layout (default: **Alt + Shift**).
- **Shift + Caps Lock** → works as normal Caps Lock (toggles caps state).
- Runs in the background with a **tray icon**:
  - Enable/Disable hotkey handling
  - Choose switching mode: `Alt+Shift`, `Ctrl+Shift`, or `Win+Space`
  - Toggle **Start with Windows**
  - Exit

### Start with Windows

The app tries to register itself for autostart using **Task Scheduler**.  
If Task Scheduler registration is blocked (common on locked-down systems), it falls back to the **HKCU Run** registry key (per-user autostart, no admin required).

## Requirements

- Windows 10/11
- Go 1.20+ (any modern Go should work)

## Build

From repo root:

```bash
go mod tidy
go build -ldflags="-H=windowsgui" -o igcapt.exe .
````

> The `windowsgui` flag builds a background app (no console window).

## Install

1. Create a stable folder (recommended):

   * `Win + R` → `%LOCALAPPDATA%`
   * Create folder: `CapsLangSwitch` (or `igcapt`)
2. Copy `igcapt.exe` into that folder, e.g.:

   * `%LOCALAPPDATA%\CapsLangSwitch\igcapt.exe`
3. Run the app (double-click).
4. Right-click the tray icon → enable **Start with Windows**.

### Updating

Replace the `.exe` **in the same folder**.
If you moved the binary, toggle **Start with Windows** off and on again to refresh the stored path.

## Usage tips

* If language switching doesn’t work, try switching the mode in the tray menu:

  * `Alt + Shift` / `Ctrl + Shift` / `Win + Space`
* Caps Lock LED/state should **not** toggle when pressing Caps alone (that means the hook is active).
* Press **Shift + Caps** to toggle caps normally.
