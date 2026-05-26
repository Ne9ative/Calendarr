package main

import (
	"log"
	"net/http"
	"os/exec"
	"time"

	"calendarr-local/internal/discovery"
)

// openCalendar finds the server on its own and opens the calendar in the
// browser. Best-effort, to be launched in a goroutine at helper startup.
//  1. server on THIS machine (single-PC install) -> localhost
//  2. otherwise, listen for the LAN beacon of a server elsewhere (multi-PC install)
func openCalendar() {
	if pingServer("http://127.0.0.1:8787/api/calendar") {
		openBrowser("http://localhost:8787")
		return
	}
	if url, ok := discovery.Listen(25 * time.Second); ok {
		openBrowser(url)
		return
	}
	log.Printf("no server detected on the network (launch server.exe on the PC that has Sonarr)")
}

func pingServer(url string) bool {
	c := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func openBrowser(url string) {
	log.Printf("opening the calendar in the browser: %s", url)
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
