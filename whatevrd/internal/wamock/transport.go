//go:build whatevr_mock

package wamock

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
)

// waHosts are the names whatsmeow and the daemon reach for. Anything matching
// is answered by the mock; anything else is refused at dial time, because a
// mock run that silently talks to the real internet is worse than one that
// fails.
var waHosts = []string{
	".whatsapp.com",
	".whatsapp.net",
	".fbcdn.net",
	".facebook.com",
}

func isWAHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range waHosts {
		if strings.HasSuffix(host, suffix) || host == strings.TrimPrefix(suffix, ".") {
			return true
		}
	}
	return false
}

// tlsIdentity is the x509 side of the mock, which is separate from the Noise
// cert chain in certs.go: this one satisfies the TLS handshake under wss://,
// that one satisfies WhatsApp's own certificate check inside it.
type tlsIdentity struct {
	pool   *x509.CertPool
	server tls.Certificate
}

func newTLSIdentity(r io.Reader, now time.Time) (*tlsIdentity, error) {
	caPub, caPriv, err := ed25519.GenerateKey(r)
	if err != nil {
		return nil, fmt.Errorf("generate ca key: %w", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "whatevr mock ca"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(r, caTmpl, caTmpl, caPub, caPriv)
	if err != nil {
		return nil, fmt.Errorf("self-sign ca: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("parse ca: %w", err)
	}

	leafPub, leafPriv, err := ed25519.GenerateKey(r)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "web.whatsapp.com"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(certValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{
			"web.whatsapp.com",
			"g.whatsapp.net",
			"mmg.whatsapp.net",
			"media.whatsapp.net",
			"localhost",
		},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	// The signer is the CA, not the leaf; x509.CreateCertificate takes the
	// parent's private key.
	leafDER, err := x509.CreateCertificate(r, leafTmpl, caCert, leafPub, caPriv)
	if err != nil {
		return nil, fmt.Errorf("sign leaf: %w", err)
	}
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafPriv)
	if err != nil {
		return nil, fmt.Errorf("marshal leaf key: %w", err)
	}
	server, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafKeyDER}),
	)
	if err != nil {
		return nil, fmt.Errorf("build tls keypair: %w", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return &tlsIdentity{pool: pool, server: server}, nil
}

// installTransport points every HTTP client in the process at the mock.
// whatsmeow's NewClient builds its websocket, pre-login and media clients by
// cloning http.DefaultTransport, so replacing that is enough to capture all
// three without touching internal/wa. It must stay an *http.Transport:
// whatsmeow type-asserts it.
//
// This is process-global and trusts a private CA, which is exactly why the
// whole package is behind the whatevr_mock build tag.
func installTransport(tlsID *tlsIdentity, addr string) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{RootCAs: tlsID.pool}
	base.ForceAttemptHTTP2 = false
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			host = address
		}
		if !isWAHost(host) && host != "127.0.0.1" && host != "localhost" && host != "::1" {
			return nil, fmt.Errorf("wamock: refusing to dial %s, mock mode reaches no real hosts", address)
		}
		return dialer.DialContext(ctx, network, addr)
	}
	http.DefaultTransport = base
}

// installCertPubKey makes whatsmeow accept the chain from certs.go. The
// variable is package-global in whatsmeow, so this affects every client in the
// process and must never run in a release binary.
func installCertPubKey(id *serverIdentity) {
	whatsmeow.WACertPubKey = *id.root.Pub
}
