// Package player launches the local video player (MPC-BE) on a file or URL.
package player

import (
	"errors"
	"os"
	"os/exec"
)

// ErrNotFound is returned when no MPC-BE executable could be located.
var ErrNotFound = errors.New("MPC-BE not found")

// candidates lists the standard MPC-BE install locations (64-bit then 32-bit).
var candidates = []string{
	`C:\Program Files\MPC-BE\mpc-be64.exe`,
	`C:\Program Files\MPC-BE x64\mpc-be64.exe`,
	`C:\Program Files (x86)\MPC-BE\mpc-be.exe`,
	`C:\Program Files (x86)\MPC-BE x86\mpc-be.exe`,
}

// FindMPCBE returns the path to the MPC-BE executable. If override points to
// an existing file, it takes priority; otherwise the known locations are tried.
func FindMPCBE(override string) string {
	if override != "" {
		if exists(override) {
			return override
		}
	}
	for _, p := range candidates {
		if exists(p) {
			return p
		}
	}
	return ""
}

// Play launches MPC-BE on target (a local path or an http URL) without blocking.
func Play(mpcPath, target string) error {
	if mpcPath == "" {
		return ErrNotFound
	}
	return exec.Command(mpcPath, target).Start()
}

func exists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
