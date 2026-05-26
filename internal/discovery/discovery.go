// Package discovery handles automatic discovery of the server on the LAN.
// The server continuously broadcasts a small UDP "beacon"; the client listens
// for it to find the calendar address without any configuration (no name or
// IP to type). Goal: the host opens server.exe, someone else opens client.exe
// on another PC and the browser opens by itself.
package discovery

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// UDP port of the beacon and magic prefix (to avoid confusion with third-party traffic).
const (
	Port  = 8786
	Magic = "CALENDARR-LOCAL"
)

// Message builds the beacon payload: "CALENDARR-LOCAL|<http port>|<pc name>".
func Message(httpPort, host string) string {
	return Magic + "|" + httpPort + "|" + host
}

// Listen listens for beacons for at most timeout and returns the HTTP URL of
// the first server heard (http://<source ip>:<announced port>). We use the
// source IP of the packet (always resolvable) rather than the name: nothing to configure.
func Listen(timeout time.Duration) (string, bool) {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{Port: Port})
	if err != nil {
		return "", false
	}
	defer pc.Close()
	_ = pc.SetReadDeadline(time.Now().Add(timeout))

	buf := make([]byte, 256)
	for {
		n, src, err := pc.ReadFromUDP(buf)
		if err != nil {
			return "", false // timeout elapsed: no server found
		}
		parts := strings.Split(string(buf[:n]), "|")
		if len(parts) < 2 || parts[0] != Magic {
			continue
		}
		if _, e := strconv.Atoi(parts[1]); e != nil {
			continue
		}
		return "http://" + src.IP.String() + ":" + parts[1], true
	}
}

// BroadcastAddrs returns the broadcast address of each active network
// interface (e.g. 192.168.1.255, 172.23.63.255...). We emit the beacon on
// ALL of them, otherwise it would only leave via the default interface
// (often a virtual WSL/Hyper-V adapter) and never reach the real LAN.
func BroadcastAddrs() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			mask := ipnet.Mask
			if len(mask) == 16 {
				mask = mask[12:] // IPv4 mask is sometimes stored as 16 bytes
			}
			if len(mask) != 4 {
				continue
			}
			bc := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bc[i] = ip4[i] | ^mask[i]
			}
			out = append(out, bc)
		}
	}
	return out
}
