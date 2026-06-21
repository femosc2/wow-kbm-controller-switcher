# WoW Profile Switcher (`wowswitch`)

Switch the live World of Warcraft Classic config between a **Keyboard & Mouse**
setup and a **ConsolePort (controller)** setup, and keep non-ConsolePort addons
synced between the two.

Works across every installed game flavor at once — **Anniversary**
(`_anniversary_`) and **Classic Era / Season of Discovery** (`_classic_era_`).
Picking a mode applies it to all installed flavors in a single run; flavors you
don't have installed are skipped automatically.

## What it does, each run

1. Verifies the game is closed (refuses to run while `WowClassic.exe` is up).
2. For each installed flavor, **syncs addons** between the active profile and the
   dormant one (rule: *add missing, newest wins*). `ConsolePort*` addons are left
   only in the ConsolePort profile and never copied into the KBM profile. Addons
   sync *within* each flavor only — never across flavors (they're different game
   versions).
3. Shows a single menu to pick **Keyboard & Mouse** or **ConsolePort**, then
   swaps the live `Interface/` and `WTF/` folders of every installed flavor
   instantly via rename.

## How it stores profiles

Inside each flavor dir, e.g. `C:\Program Files (x86)\World of Warcraft\_anniversary_`
(and `..._classic_era_`):

- Live (what WoW reads): `Interface/`, `WTF/`.
- Dormant copy of the inactive mode: `Interface.<mode>/`, `WTF.<mode>/`
  (`<mode>` is `kbm` or `consoleport`).
- `.wowswitch_active` — a marker file recording which mode is currently live.

The **active** mode's folders are always the plain `Interface`/`WTF`; only the
**inactive** mode keeps a suffixed copy. A switch renames the two pairs.

## First run (bootstrap)

On the very first run a flavor has only the ConsolePort config. The tool assumes
the current setup is **ConsolePort**, then clones it into a **KBM** profile
(`Interface.kbm` / `WTF.kbm`) and removes the `ConsolePort*` addon folders from
that KBM copy. The cloned `WTF` keeps your account/realm/character data and login;
set up your keyboard keybinds once the first time you play in KBM mode. This
bootstrap runs independently per flavor.

## Get the binary

`wowswitch.exe` is **not committed** to the repo — it's a build artifact.

- **Download from CI:** every push builds it on GitHub Actions. Grab the latest
  `wowswitch-windows-amd64` artifact from the
  [Actions tab](../../actions) (artifacts expire after ~90 days).
- **Or build it yourself** — requires Go (1.21+); from this folder:

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
- **Override the install location** with the `WOW_ROOT` environment variable
  (point it at your `World of Warcraft` folder) if your install differs. For a
  single explicit flavor dir, set `WOW_BASE` instead.
- **Interrupted switch / inconsistent state:** the tool detects when `Interface`
  or `WTF` is missing or duplicated and prints exactly which folder to rename
  back. Nothing is deleted during a switch — only renamed — so recovery is just
  renaming a `*.kbm` / `*.consoleport` folder back to `Interface` / `WTF`.
```
