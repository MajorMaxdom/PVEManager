// Package tlsutil creates and loads the self-signed certificate used when no cert is configured.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SelfSigned returns a keypair stored in dir, generating one (10 years) if absent.
func SelfSigned(dir, hostname string) (tls.Certificate, error) {
	crt, key := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if c, err := tls.LoadX509KeyPair(crt, key); err == nil {
		return c, nil
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	sn, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 100))
	t := &x509.Certificate{
		SerialNumber: sn, Subject: pkix.Name{CommonName: "pve-manager"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	if hostname != "" {
		t.DNSNames = append(t.DNSNames, hostname)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() {
				t.IPAddresses = append(t.IPAddresses, ipn.IP)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	if err := os.WriteFile(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(crt, key)
}
