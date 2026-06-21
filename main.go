// wowswitch — switch the live WoW Classic config between a keyboard/mouse
// setup and a ConsolePort (controller) setup, across every installed game
// flavor (Anniversary and Classic Era / Season of Discovery), and keep
// non-ConsolePort addons synced between the two profiles within each flavor.
//
// On each run it, for each installed flavor: (1) syncs non-ConsolePort addons
// between the active and dormant profile, then after showing a single menu it
// swaps the live Interface/WTF folders via an instant rename. The chosen mode
// is applied to all installed flavors at once.
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

// defaultRoot is the World of Warcraft install folder that contains the
// per-flavor directories (_anniversary_, _classic_era_, ...). Override at
// runtime with the WOW_ROOT environment variable if the game lives elsewhere.
const defaultRoot = `C:\Program Files (x86)\World of Warcraft`

// flavorDirs are the game flavor subdirectories managed by this tool. Each
// holds its own Interface/ and WTF/. Missing ones are skipped at runtime.
var flavorDirs = []string{"_anniversary_", "_classic_era_"}

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

// bases returns the flavor base directories the tool should operate on.
//
// WOW_BASE (single explicit flavor dir) takes precedence for back-compat and
// testing; otherwise every flavor under WOW_ROOT (or the default root) is
// returned, present or not — run() filters to the ones that exist.
func bases() []string {
	if v := strings.TrimSpace(os.Getenv("WOW_BASE")); v != "" {
		return []string{v}
	}
	root := defaultRoot
	if v := strings.TrimSpace(os.Getenv("WOW_ROOT")); v != "" {
		root = v
	}
	out := make([]string, 0, len(flavorDirs))
	for _, f := range flavorDirs {
		out = append(out, filepath.Join(root, f))
	}
	return out
}

// flavorName turns a flavor base dir into a friendly display name.
func flavorName(base string) string {
	n := strings.ToLower(strings.Trim(filepath.Base(base), "_"))
	switch n {
	case "anniversary":
		return "Anniversary"
	case "classic_era":
		return "Classic Era (Season of Discovery)"
	default:
		return filepath.Base(base)
	}
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

func livePath(base, folder string) string { return filepath.Join(base, folder) }

func dormantPath(base, folder, mode string) string {
	return filepath.Join(base, folder+"."+mode)
}

func markerPath(base string) string { return filepath.Join(base, markerFile) }

// ---- State (marker file) -------------------------------------------------

func readActiveMode(base string) (string, error) {
	b, err := os.ReadFile(markerPath(base))
	if err != nil {
		return "", err
	}
	m := strings.TrimSpace(string(b))
	if m != modeKBM && m != modeCP {
		return "", fmt.Errorf("marker file contains unexpected value %q", m)
	}
	return m, nil
}

func writeActiveMode(base, mode string) error {
	return os.WriteFile(markerPath(base), []byte(mode+"\n"), 0o644)
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

// resolveActiveMode reads the marker for a flavor, or on a fresh install
// assumes the current live config is ConsolePort and seeds a KBM profile by
// cloning it (minus the ConsolePort* addons). It is idempotent.
func resolveActiveMode(base string) (string, error) {
	if mode, err := readActiveMode(base); err == nil {
		return mode, nil
	}

	// First run: live folders must be present.
	for _, f := range bundleFolders {
		if !isDir(livePath(base, f)) {
			return "", fmt.Errorf("expected live folder %q not found under %s", f, base)
		}
	}

	active := modeCP // current setup is the ConsolePort one
	inactive := modeKBM

	fmt.Printf("First run detected. Treating current config as %s.\n", label(active))
	fmt.Printf("Seeding a %s profile from it (this clones Interface + WTF once)...\n", label(inactive))

	for _, f := range bundleFolders {
		dst := dormantPath(base, f, inactive)
		if exists(dst) {
			continue // already seeded (idempotent)
		}
		if err := copyTree(livePath(base, f), dst); err != nil {
			return "", fmt.Errorf("seeding %s: %w", dst, err)
		}
	}

	// Strip ConsolePort* addons from the freshly seeded KBM profile.
	if err := stripConsolePortAddons(filepath.Join(dormantPath(base, "Interface", inactive), "AddOns")); err != nil {
		return "", fmt.Errorf("stripping ConsolePort addons from KBM profile: %w", err)
	}

	if err := writeActiveMode(base, active); err != nil {
		return "", err
	}
	fmt.Println("Seed complete.")
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

// validateState checks the on-disk invariant for a flavor and returns a
// descriptive error (with recovery guidance) if it is broken.
func validateState(base, active string) error {
	inactive := other(active)
	for _, f := range bundleFolders {
		if !isDir(livePath(base, f)) {
			return fmt.Errorf("live folder %q is missing — a previous switch may have been interrupted.\n"+
				"  Recover by renaming %s back to %s", f, dormantPath(base, f, active), livePath(base, f))
		}
		if !isDir(dormantPath(base, f, inactive)) {
			return fmt.Errorf("dormant folder %q is missing", dormantPath(base, f, inactive))
		}
		if exists(dormantPath(base, f, active)) {
			return fmt.Errorf("both live %q and %q exist — inconsistent state from an interrupted switch.\n"+
				"  Inspect those folders and remove/rename the stale one before retrying", f, dormantPath(base, f, active))
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

func switchProfile(base, from, to string) error {
	// Pre-checks before touching anything.
	for _, f := range bundleFolders {
		if !isDir(dormantPath(base, f, to)) {
			return fmt.Errorf("target profile folder %q does not exist", dormantPath(base, f, to))
		}
		if !isDir(livePath(base, f)) {
			return fmt.Errorf("live folder %q does not exist", livePath(base, f))
		}
		if exists(dormantPath(base, f, from)) {
			return fmt.Errorf("stash target %q already exists; refusing to overwrite", dormantPath(base, f, from))
		}
	}

	// Stash the live (active) folders, then promote the target folders.
	for _, f := range bundleFolders {
		if err := os.Rename(livePath(base, f), dormantPath(base, f, from)); err != nil {
			return fmt.Errorf("stashing %s: %w", f, err)
		}
	}
	for _, f := range bundleFolders {
		if err := os.Rename(dormantPath(base, f, to), livePath(base, f)); err != nil {
			return fmt.Errorf("activating %s (PARTIAL STATE — see folders under %s): %w", f, base, err)
		}
	}

	return writeActiveMode(base, to)
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

// flavorState carries a prepared (bootstrapped, validated, synced) flavor into
// the switch phase.
type flavorState struct {
	base   string
	name   string
	active string
}

func run() error {
	fmt.Println("=== WoW Profile Switcher ===")

	all := bases()
	var present []string
	for _, b := range all {
		if isDir(b) {
			present = append(present, b)
		} else {
			fmt.Printf("Skipping (not installed): %s\n", b)
		}
	}
	if len(present) == 0 {
		return fmt.Errorf("no WoW flavor directories found.\n  Looked for: %s\n"+
			"  Set WOW_ROOT to your World of Warcraft install folder", strings.Join(all, ", "))
	}

	if isWowRunning() {
		return fmt.Errorf("WowClassic.exe is currently running.\n  Close the game completely before switching profiles")
	}

	// Prepare each flavor: bootstrap, validate, sync addons.
	var states []flavorState
	var totalCopied, totalUpdated int
	for _, b := range present {
		name := flavorName(b)
		fmt.Printf("\n--- %s ---\n", name)
		fmt.Printf("Game dir: %s\n", b)

		active, err := resolveActiveMode(b)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := validateState(b, active); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		inactive := other(active)
		fmt.Printf("Current mode: %s\n", label(active))

		fmt.Print("Syncing addons (excluding ConsolePort)... ")
		copied, updated, err := syncAddons(
			filepath.Join(livePath(b, "Interface"), "AddOns"),
			filepath.Join(dormantPath(b, "Interface", inactive), "AddOns"),
		)
		if err != nil {
			fmt.Println("FAILED")
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Printf("done (%d copied, %d updated).\n", copied, updated)
		totalCopied += copied
		totalUpdated += updated

		states = append(states, flavorState{base: b, name: name, active: active})
	}

	if totalCopied > 0 || totalUpdated > 0 {
		fmt.Println()
		fmt.Println("Note: addon uninstalls are not propagated — remove an addon from")
		fmt.Println("both profiles to delete it everywhere. Addons sync within each")
		fmt.Println("flavor only, never across flavors.")
	}
	fmt.Println()

	// Single menu; the chosen mode is applied to every installed flavor.
	fmt.Println("Choose a profile to activate (applies to all flavors above):")
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

	fmt.Println()
	var switched, already int
	for _, st := range states {
		if st.active == target {
			fmt.Printf("%s: %s already active.\n", st.name, label(target))
			already++
			continue
		}
		fmt.Printf("%s: switching to %s... ", st.name, label(target))
		if err := switchProfile(st.base, st.active, target); err != nil {
			fmt.Println("FAILED")
			return fmt.Errorf("%s: %w", st.name, err)
		}
		fmt.Println("done.")
		switched++
	}

	fmt.Printf("\nActive profile is now: %s (%d switched, %d already set).\n", label(target), switched, already)
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
