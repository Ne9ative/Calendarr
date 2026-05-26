//go:build windows

// Package desktop provides small Windows utilities for desktop integration:
// auto-start (Run key in the registry), opening the browser or a terminal,
// and an error message box (useful when the app has no console).
package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// AutoStartEnabled reports whether the app is launched at Windows startup.
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

// SetAutoStartPath enables or disables auto-start for a specific exe. Used by
// the installer, which needs to register server.exe rather than itself.
func SetAutoStartPath(name, exePath string, enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(name); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	return k.SetStringValue(name, `"`+exePath+`"`)
}

// OpenBrowser opens a URL in the default browser.
func OpenBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// OpenTerminal opens a PowerShell window that tails the log live. We use a
// small temporary script (.ps1) rather than a -Command: it is more robust (no
// quoting issues) and the window always shows a header, so it is never empty
// even if the log is momentarily missing.
func OpenTerminal(logPath string) {
	p := strings.ReplaceAll(logPath, "'", "''")
	script := "$Host.UI.RawUI.WindowTitle = 'Calendarr - server log'\n" +
		"[Console]::OutputEncoding = [Text.Encoding]::UTF8\n" +
		"$log = '" + p + "'\n" +
		"Write-Host '=== Calendarr log (live, Ctrl+C to close) ===' -ForegroundColor Cyan\n" +
		"Write-Host $log -ForegroundColor DarkGray\n" +
		"while (-not (Test-Path -LiteralPath $log)) { Write-Host 'Waiting for the log...' -ForegroundColor Yellow; Start-Sleep -Seconds 1 }\n" +
		"Get-Content -LiteralPath $log -Tail 500 -Wait -Encoding UTF8\n"
	tmp := filepath.Join(os.TempDir(), "calendarr-log.ps1")
	if err := os.WriteFile(tmp, []byte(script), 0o644); err != nil {
		return
	}
	cmd := exec.Command("powershell", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", tmp)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	_ = cmd.Start()
}

// MessageBox displays a native error dialog (since the app has no console).
func MessageBox(title, text string) {
	proc := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	proc.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x10) // MB_ICONERROR
}
