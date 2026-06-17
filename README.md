# WoW Profile Switcher (`wowswitch`)

Switch the live World of Warcraft Classic config between a **Keyboard & Mouse**
setup and a **ConsolePort (controller)** setup, and keep non-ConsolePort addons
synced between the two.

## What it does, each run

1. Verifies the game is closed (refuses to run while `WowClassic.exe` is up).
2. **Syncs addons** between the active profile and the dormant one
   (rule: *add missing, newest wins*). `ConsolePort*` addons are left only in
   the ConsolePort profile and never copied into the KBM profile.
3. Shows a menu to pick **Keyboard & Mouse** or **ConsolePort**, then swaps the
   live `Interface/` and `WTF/` folders instantly via rename.

## How it stores profiles

Inside `C:\Program Files (x86)\World of Warcraft\_anniversary_`:

- Live (what WoW reads): `Interface/`, `WTF/`.
- Dormant copy of the inactive mode: `Interface.<mode>/`, `WTF.<mode>/`
  (`<mode>` is `kbm` or `consoleport`).
- `.wowswitch_active` — a marker file recording which mode is currently live.

The **active** mode's folders are always the plain `Interface`/`WTF`; only the
**inactive** mode keeps a suffixed copy. A switch renames the two pairs.

## First run (bootstrap)

On the very first run there is only the ConsolePort config. The tool assumes the
current setup is **ConsolePort**, then clones it into a **KBM** profile
(`Interface.kbm` / `WTF.kbm`) and removes the `ConsolePort*` addon folders from
that KBM copy. The cloned `WTF` keeps your account/realm/character data and login;
set up your keyboard keybinds once the first time you play in KBM mode.

## Build

Requires Go (1.21+). From this folder:

```
go build -o wowswitch.exe
```

Then run `wowswitch.exe` (double-click or from a terminal). Make a Desktop
shortcut to it for convenience.

## Notes & recovery

- **No admin needed** — WoW already writes to this folder as your normal user.
- **Addon uninstalls are not propagated.** Removing an addon from one profile
  alone makes it reappear on next sync (copied back from the other). Delete it
  from both to remove it everywhere.
- **Override the game path** with the `WOW_BASE` environment variable if your
  install differs.
- **Interrupted switch / inconsistent state:** the tool detects when `Interface`
  or `WTF` is missing or duplicated and prints exactly which folder to rename
  back. Nothing is deleted during a switch — only renamed — so recovery is just
  renaming a `*.kbm` / `*.consoleport` folder back to `Interface` / `WTF`.
```
