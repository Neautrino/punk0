package main

import (
	"net"
	"net/http"
	"strings"
)

// DNS rebinding: a page on the attacker's domain re-resolves that domain to
// 127.0.0.1 and then reads punk0's API as same-origin, under its own Host.
// localPost already shuts this out for writes; hostIsLocal extends it to every
// request whenever punk0 only listens on this machine.

// hostIsLocal reports whether a request's Host names this machine directly:
// localhost or an IP literal. A rebound page always arrives under a domain name.
func hostIsLocal(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || net.ParseIP(host) != nil
}

// loopbackBind reports whether addr, the -host value, accepts connections from
// this machine only. Anything else is remote mode: the user chose to be
// reachable, possibly under a hostname (Tailscale, a reverse proxy), so the
// Host check would only lock them out.
func loopbackBind(addr string) bool {
	if addr == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(addr, "[]"))
	return ip != nil && ip.IsLoopback()
}

// refuseRebound answers 403 and returns true when localOnly is set and r did
// not address this machine by localhost or IP.
func refuseRebound(localOnly bool, w http.ResponseWriter, r *http.Request) bool {
	if !localOnly || hostIsLocal(r.Host) {
		return false
	}
	fail(w, http.StatusForbidden, "open punk0 by IP address or localhost")
	return true
}
