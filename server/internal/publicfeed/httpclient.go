package publicfeed

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed cacerts.pem
var bundledCACerts []byte

// newAndroidSafeHTTPClient builds an HTTP client that works reliably on
// Android 6.0.1 with CGO_ENABLED=0:
//   - Uses bundled up-to-date CA roots (Android 6 lacks /etc/ssl/certs and has 2019 certs)
//   - Uses explicit DNS servers (FRMAEO_DNS from Android WifiManager, falling back to 1.1.1.1 / 8.8.8.8)
//     because Android has no /etc/resolv.conf.
func newAndroidSafeHTTPClient() *http.Client {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(bundledCACerts)

	dnsServers := configuredDNSServers()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 4 * time.Second}
			var lastErr error
			for _, srv := range dnsServers {
				conn, err := d.DialContext(ctx, "udp", srv)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  resolver,
	}

	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        8,
		IdleConnTimeout:     30 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   25 * time.Second,
	}
}

func configuredDNSServers() []string {
	var out []string
	if env := strings.TrimSpace(os.Getenv("FRMAEO_DNS")); env != "" {
		for _, p := range strings.Split(env, ",") {
			p = strings.TrimSpace(p)
			if p == "" || p == "0.0.0.0" {
				continue
			}
			if !strings.Contains(p, ":") {
				p += ":53"
			}
			out = append(out, p)
		}
	}
	out = append(out, "1.1.1.1:53", "8.8.8.8:53")
	return out
}
