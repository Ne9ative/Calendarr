//go:build windows

package discovery

import (
	"net"
	"syscall"
	"time"
)

// Broadcast continuously emits the beacon (every 2s) on ALL network
// interfaces. Blocking: must be launched in a goroutine. Requires
// SO_BROADCAST, otherwise Windows refuses to send to a broadcast address.
func Broadcast(httpPort, host string) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return err
	}
	defer conn.Close()

	if rc, e := conn.SyscallConn(); e == nil {
		_ = rc.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
		})
	}

	msg := []byte(Message(httpPort, host))
	for {
		// One emission per interface (192.168.x.255, etc.) plus the global
		// broadcast as a safety net. If no interface is found, at least the global one.
		for _, bc := range BroadcastAddrs() {
			_, _ = conn.WriteToUDP(msg, &net.UDPAddr{IP: bc, Port: Port})
		}
		_, _ = conn.WriteToUDP(msg, &net.UDPAddr{IP: net.IPv4bcast, Port: Port})
		time.Sleep(2 * time.Second)
	}
}
