// wowswitch — switch the live WoW Classic config between a keyboard/mouse
// setup and a ConsolePort (controller) setup, and keep non-ConsolePort addons
// synced between the two profiles.
//
// On each run it: (1) syncs non-ConsolePort addons between the active and the
// dormant profile, then (2) shows a menu to pick KBM or ConsolePort and swaps
// the live Interface/WTF folders via an instant rename.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---- Configuration -------------------------------------------------------

// defaultBase is the game flavor directory that actually contains Interface/
// and WTF/. Override at runtime with the WOW_BASE environment variable if the
// game ever lives somewhere else.
const defaultBase = `C:\Program Files (x86)\World of Warcraft\_anniversary_`

const (
	modeKBM = "kbm"
	modeCP  = "consoleport"

	markerFile = ".wowswitch_active"

	// Addon folders whose name starts with this prefix (case-insensitive) are
	// treated as ConsolePort-exclusive: never synced into the KBM profile.
	consolePortPrefix = "consoleport"

	// mtimes within this slack of each other count as "equal" (copies and
	// different filesystems jitter timestamps slightly).
	mtimeSlack = 2 * time.Second
)

// bundleFolders are swapped together as one profile.
var bundleFolders = []string{"Interface", "WTF"}

var stdin = bufio.NewReader(os.Stdin)

func base() string {
	if v := strings.TrimSpace(os.Getenv("WOW_BASE")); v != "" {
		return v
	}
	return defaultBase
}

func label(mode string) string {
	switch mode {
	case modeKBM:
		return "Keyboard & Mouse"
	case modeCP:
		return "ConsolePort (controller)"
	default:
		return mode
	}
}

func other(mode string) string {
	if mode == modeKBM {
		return modeCP
	}
	return modeKBM
}

// ---- Path helpers --------------------------------------------------------

func livePath(folder string) string { return filepath.Join(base(), folder) }

func dormantPath(folder, mode string) string {
	return filepath.Join(base(), folder+"."+mode)
}

func markerPath() string { return filepath.Join(base(), markerFile) }

// ---- State (marker file) -------------------------------------------------

func readActiveMode() (string, error) {
	b, err := os.ReadFile(markerPath())
	if err != nil {
		return "", err
	}
	m := strings.TrimSpace(string(b))
	if m != modeKBM && m != modeCP {
		return "", fmt.Errorf("marker file contains unexpected value %q", m)
	}
	return m, nil
}

func writeActiveMode(mode string) error {
	return os.WriteFile(markerPath(), []byte(mode+"\n"), 0o644)
}

// ---- Filesystem helpers --------------------------------------------------

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func removeTree(path string) error { return os.RemoveAll(path) }

// copyTree recursively copies src to dst, preserving file modification times so
// that "newest wins" comparisons stay stable across runs.
func copyTree(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return copyFile(src, dst, fi)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyTree(s, d); err != nil {
				return err
			}
		} else {
			info, err := e.Info()
			if err != nil {
				return err
			}
			if err := copyFile(s, d, info); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Preserve mtime; ignore failure (non-fatal for behavior).
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

// newestModTime returns the most recent modification time of any file under
// root. Returns the zero time if root is empty or missing.
func newestModTime(root string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

func isConsolePortAddon(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), consolePortPrefix)
}

// ---- WoW running guard ---------------------------------------------------

func isWowRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq WowClassic.exe", "/NH").Output()
	if err != nil {
		return false // can't determine; don't block the user
	}
	return strings.Contains(strings.ToLower(string(out)), "wowclassic.exe")
}

// ---- Bootstrap & state validation ----------------------------------------

// resolveActiveMode reads the marker, or on a fresh install assumes the current
// live config is ConsolePort and seeds a KBM profile by cloning it (minus the
// ConsolePort* addons). It is idempotent.
func resolveActiveMode() (string, error) {
	if mode, err := readActiveMode(); err == nil {
		return mode, nil
	}

	// First run: live folders must be present.
	for _, f := range bundleFolders {
		if !isDir(livePath(f)) {
			return "", fmt.Errorf("expected live folder %q not found under %s", f, base())
		}
	}

	active := modeCP // current setup is the ConsolePort one
	inactive := modeKBM

	fmt.Printf("First run detected. Treating current config as %s.\n", label(active))
	fmt.Printf("Seeding a %s profile from it (this clones Interface + WTF once)...\n", label(inactive))

	for _, f := range bundleFolders {
		dst := dormantPath(f, inactive)
		if exists(dst) {
			continue // already seeded (idempotent)
		}
		if err := copyTree(livePath(f), dst); err != nil {
			return "", fmt.Errorf("seeding %s: %w", dst, err)
		}
	}

	// Strip ConsolePort* addons from the freshly seeded KBM profile.
	if err := stripConsolePortAddons(filepath.Join(dormantPath("Interface", inactive), "AddOns")); err != nil {
		return "", fmt.Errorf("stripping ConsolePort addons from KBM profile: %w", err)
	}

	if err := writeActiveMode(active); err != nil {
		return "", err
	}
	fmt.Println("Seed complete.")
	fmt.Println()
	return active, nil
}

func stripConsolePortAddons(addonsDir string) error {
	entries, err := os.ReadDir(addonsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() && isConsolePortAddon(e.Name()) {
			if err := removeTree(filepath.Join(addonsDir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateState checks the on-disk invariant and returns a descriptive error
// (with recovery guidance) if it is broken.
func validateState(active string) error {
	inactive := other(active)
	for _, f := range bundleFolders {
		if !isDir(livePath(f)) {
			return fmt.Errorf("live folder %q is missing — a previous switch may have been interrupted.\n"+
				"  Recover by renaming %s back to %s", f, dormantPath(f, active), livePath(f))
		}
		if !isDir(dormantPath(f, inactive)) {
			return fmt.Errorf("dormant folder %q is missing", dormantPath(f, inactive))
		}
		if exists(dormantPath(f, active)) {
			return fmt.Errorf("both live %q and %q exist — inconsistent state from an interrupted switch.\n"+
				"  Inspect those folders and remove/rename the stale one before retrying", f, dormantPath(f, active))
		}
	}
	return nil
}

// ---- Addon sync ----------------------------------------------------------

func dirSet(dir string) (map[string]bool, error) {
	set := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return set, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() && !isConsolePortAddon(e.Name()) {
			set[e.Name()] = true
		}
	}
	return set, nil
}

// syncAddons mirrors non-ConsolePort addons between two AddOns dirs using the
// "add missing, newest wins" rule. Returns (copied, updated) counts.
func syncAddons(aDir, bDir string) (copied, updated int, err error) {
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		return 0, 0, err
	}

	aSet, err := dirSet(aDir)
	if err != nil {
		return 0, 0, err
	}
	bSet, err := dirSet(bDir)
	if err != nil {
		return 0, 0, err
	}

	names := map[string]bool{}
	for n := range aSet {
		names[n] = true
	}
	for n := range bSet {
		names[n] = true
	}

	for name := range names {
		aPath := filepath.Join(aDir, name)
		bPath := filepath.Join(bDir, name)

		switch {
		case aSet[name] && !bSet[name]:
			if err := copyTree(aPath, bPath); err != nil {
				return copied, updated, fmt.Errorf("copying %s to inactive profile: %w", name, err)
			}
			copied++
		case bSet[name] && !aSet[name]:
			if err := copyTree(bPath, aPath); err != nil {
				return copied, updated, fmt.Errorf("copying %s to active profile: %w", name, err)
			}
			copied++
		default: // present on both — newest wins
			at := newestModTime(aPath)
			bt := newestModTime(bPath)
			diff := at.Sub(bt)
			if diff < 0 {
				diff = -diff
			}
			if diff <= mtimeSlack {
				continue // effectively identical
			}
			var src, dst string
			if at.After(bt) {
				src, dst = aPath, bPath
			} else {
				src, dst = bPath, aPath
			}
			if err := removeTree(dst); err != nil {
				return copied, updated, fmt.Errorf("replacing %s: %w", name, err)
			}
			if err := copyTree(src, dst); err != nil {
				return copied, updated, fmt.Errorf("updating %s: %w", name, err)
			}
			updated++
		}
	}
	return copied, updated, nil
}

// ---- Profile switch ------------------------------------------------------

func switchProfile(from, to string) error {
	// Pre-checks before touching anything.
	for _, f := range bundleFolders {
		if !isDir(dormantPath(f, to)) {
			return fmt.Errorf("target profile folder %q does not exist", dormantPath(f, to))
		}
		if !isDir(livePath(f)) {
			return fmt.Errorf("live folder %q does not exist", livePath(f))
		}
		if exists(dormantPath(f, from)) {
			return fmt.Errorf("stash target %q already exists; refusing to overwrite", dormantPath(f, from))
		}
	}

	// Stash the live (active) folders, then promote the target folders.
	for _, f := range bundleFolders {
		if err := os.Rename(livePath(f), dormantPath(f, from)); err != nil {
			return fmt.Errorf("stashing %s: %w", f, err)
		}
	}
	for _, f := range bundleFolders {
		if err := os.Rename(dormantPath(f, to), livePath(f)); err != nil {
			return fmt.Errorf("activating %s (PARTIAL STATE — see folders under %s): %w", f, base(), err)
		}
	}

	return writeActiveMode(to)
}

// ---- Menu / main ---------------------------------------------------------

func prompt(msg string) string {
	fmt.Print(msg)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

func pause() {
	fmt.Print("\nPress Enter to close...")
	_, _ = stdin.ReadString('\n')
}

func run() error {
	fmt.Println("=== WoW Profile Switcher ===")
	fmt.Printf("Game dir: %s\n\n", base())

	if !isDir(base()) {
		return fmt.Errorf("game directory not found: %s\n  Set WOW_BASE to the correct _flavor_ folder", base())
	}

	if isWowRunning() {
		return fmt.Errorf("WowClassic.exe is currently running.\n  Close the game completely before switching profiles")
	}

	active, err := resolveActiveMode()
	if err != nil {
		return err
	}
	if err := validateState(active); err != nil {
		return err
	}

	inactive := other(active)
	fmt.Printf("Current mode: %s\n", label(active))

	// Sync non-ConsolePort addons between the live and dormant profiles.
	fmt.Print("Syncing addons (excluding ConsolePort)... ")
	copied, updated, err := syncAddons(
		filepath.Join(livePath("Interface"), "AddOns"),
		filepath.Join(dormantPath("Interface", inactive), "AddOns"),
	)
	if err != nil {
		fmt.Println("FAILED")
		return err
	}
	fmt.Printf("done (%d copied, %d updated).\n", copied, updated)
	if copied > 0 || updated > 0 {
		fmt.Println("Note: addon uninstalls are not propagated — remove an addon from")
		fmt.Println("both profiles if you want it gone everywhere.")
	}
	fmt.Println()

	// Menu.
	fmt.Println("Choose a profile to activate:")
	fmt.Printf("  [1] %s\n", label(modeKBM))
	fmt.Printf("  [2] %s\n", label(modeCP))
	fmt.Println("  [q] Quit (no change)")

	var target string
	switch prompt("> ") {
	case "1":
		target = modeKBM
	case "2":
		target = modeCP
	case "q", "Q", "":
		fmt.Println("No change made.")
		return nil
	default:
		fmt.Println("Unrecognized choice — no change made.")
		return nil
	}

	if target == active {
		fmt.Printf("%s is already active. Nothing to do.\n", label(active))
		return nil
	}

	fmt.Printf("Switching to %s... ", label(target))
	if err := switchProfile(active, target); err != nil {
		fmt.Println("FAILED")
		return err
	}
	fmt.Println("done.")
	fmt.Printf("Active profile is now: %s\n", label(target))
	return nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
	}
	pause()
	if err != nil {
		os.Exit(1)
	}
}
