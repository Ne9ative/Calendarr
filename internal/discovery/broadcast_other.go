//go:build !windows

package discovery

// Broadcast: auto-discovery is not supported outside Windows (the app targets
// Windows). Stub so that the build succeeds on other operating systems.
func Broadcast(httpPort, host string) error { return nil }
