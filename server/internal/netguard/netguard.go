// Package netguard builds HTTP clients for user-supplied URLs (cron
// webhooks, uptime checks) that cannot reach private networks, so a tenant
// cannot use them to probe the install's own services.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

var ErrBlocked = errors.New("the address is on a private network; an admin can allow private targets")

// Private reports loopback, private, link-local, CGNAT and unspecified addresses.
func Private(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64 {
		return true // 100.64.0.0/10
	}
	return false
}

// Client returns an HTTP client. Unless allowPrivate is set, connections to
// private addresses fail, including after redirects and DNS tricks, because
// the check runs on the resolved address at connect time.
func Client(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	if !allowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || Private(ip) {
				return ErrBlocked
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			Proxy:               nil,
			TLSHandshakeTimeout: timeout,
			MaxIdleConns:        10,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// CheckURL validates a target URL's shape.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("the URL must be http or https with a host")
	}
	return nil
}

// Do sends a request with the client and a context timeout.
func Do(ctx context.Context, c *http.Client, req *http.Request) (*http.Response, error) {
	return c.Do(req.WithContext(ctx))
}
