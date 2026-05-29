//go:build windows

// Package desktop provides small Windows utilities for desktop integration:
// per-user auto-start (HKCU Run key), opening the browser via ShellExecute,
// and an error message box (useful when the app has no console).
//
// Auto-start uses the per-user "Run" key
// (HKCU\Software\Microsoft\Windows\CurrentVersion\Run): it never needs
// elevation (HKCU is always writable by the current user), unlike registering
// a Task Scheduler task in the root folder, which is denied (E_ACCESSDENIED)
// for a standard, non-elevated process.
package desktop

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// runKey is the per-user autostart key. Values here are launched once at logon.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// AutoStartEnabled reports whether an autostart entry with this name exists for
// the current user.
func AutoStartEnabled(name string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(name)
	return err == nil
}

// SetAutoStart enables or disables auto-start for the current executable.
func SetAutoStart(name string, enabled bool) error {
	exe := ""
	if enabled {
		e, err := os.Executable()
		if err != nil {
			return err
		}
		exe = e
	}
	return SetAutoStartPath(name, exe, enabled)
}

// SetAutoStartPath enables or disables auto-start for a specific exe path.
// Used by the installer, which needs to register the eventual location of the
// binary rather than its own path.
func SetAutoStartPath(name, exePath string, enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			return nil // no Run key / nothing to remove — already "disabled"
		}
		defer k.Close()
		if err := k.DeleteValue(name); err != nil && err != registry.ErrNotExist &&
			err != syscall.ERROR_FILE_NOT_FOUND {
			return err
		}
		return nil
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// Quote the path so Windows parses it correctly even if it contains spaces.
	val := exePath
	if !strings.HasPrefix(val, `"`) {
		val = `"` + exePath + `"`
	}
	return k.SetStringValue(name, val)
}

// RefreshAutoStart re-syncs the autostart entry to the current executable path
// if it is enabled. No-op if disabled. Lets the user move or rebuild the .exe
// transparently — the next launch rewrites the value so a stale path is healed.
func RefreshAutoStart(name string) error {
	if !AutoStartEnabled(name) {
		return nil
	}
	return SetAutoStart(name, true)
}

// OpenBrowser opens a URL in the user's default browser via ShellExecute
// (the same API the Start menu uses to launch shortcuts).
func OpenBrowser(url string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(url)
	_ = windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// MessageBox displays a native error dialog. user32!MessageBoxW is imported
// statically through golang.org/x/sys/windows — no runtime LoadLibrary.
func MessageBox(title, text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	_, _ = windows.MessageBox(0, t, c, 0x10) // MB_ICONERROR
}
