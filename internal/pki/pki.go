// Package pki runs the server's private certificate authority.
package pki

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

const (
	caKeyBits   = 3072
	leafKeyBits = 2048
	caValidity  = 10 * 365 * 24 * time.Hour
	// Apple caps TLS server certificates at 825 days; 397 keeps us well inside every client limit.
	serverValidity = 397 * 24 * time.Hour
	backdate       = time.Hour
	clientBackdate = 12 * time.Hour // TAK Server default; tolerates clock skew on phones
)

var ErrNoCA = errors.New("brak urzędu certyfikacji (CA) — dokończ konfigurację serwera")

type PKI struct {
	dir string

	mu         sync.RWMutex
	caCert     *x509.Certificate
	caKey      *rsa.PrivateKey
	caPEM      []byte
	serverCert *tls.Certificate
	serverX509 *x509.Certificate
}

func New(dir string) (*PKI, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := &PKI{dir: dir}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PKI) path(name string) string { return filepath.Join(p.dir, name) }

func (p *PKI) load() error {
	caPEM, err := os.ReadFile(p.path("ca.crt"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(p.path("ca.key"))
	if err != nil {
		return err
	}
	cert, err := parseCertPEM(caPEM)
	if err != nil {
		return fmt.Errorf("ca.crt: %w", err)
	}
	key, err := parseKeyPEM(keyPEM)
	if err != nil {
		return fmt.Errorf("ca.key: %w", err)
	}
	p.caCert, p.caKey, p.caPEM = cert, key, caPEM

	if srv, err := tls.LoadX509KeyPair(p.path("server.crt"), p.path("server.key")); err == nil {
		leaf, _ := x509.ParseCertificate(srv.Certificate[0])
		p.serverCert, p.serverX509 = &srv, leaf
	}
	return nil
}

func (p *PKI) HasCA() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.caCert != nil
}

func (p *PKI) CACert() *x509.Certificate {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.caCert
}

func (p *PKI) CAPEM() []byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.caPEM
}

func (p *PKI) CAPool() *x509.CertPool {
	pool := x509.NewCertPool()
	if c := p.CACert(); c != nil {
		pool.AddCert(c)
	}
	return pool
}

// InitCA creates the root CA unless one already exists.
func (p *PKI) InitCA(name, org string) (*x509.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caCert != nil {
		return p.caCert, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, caKeyBits)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          newSerial(),
		Subject:               pkix.Name{CommonName: name + " Root CA", Organization: []string{org}, OrganizationalUnit: []string{"ChameleonTAK"}},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		SubjectKeyId:          keyID(&key.PublicKey),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeFile(p.path("ca.key"), encodeKey(key), 0o600); err != nil {
		return nil, err
	}
	if err := writeFile(p.path("ca.crt"), certPEM, 0o644); err != nil {
		return nil, err
	}
	p.caCert, p.caKey, p.caPEM = cert, key, certPEM
	return cert, nil
}

// IssueServer (re)creates the TLS certificate used by every listener.
func (p *PKI) IssueServer(cn string, dnsNames []string, ips []net.IP) (*x509.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caCert == nil {
		return nil, ErrNoCA
	}
	key, err := rsa.GenerateKey(rand.Reader, leafKeyBits)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	notAfter := now.Add(serverValidity)
	if notAfter.After(p.caCert.NotAfter) {
		notAfter = p.caCert.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber:   newSerial(),
		Subject:        pkix.Name{CommonName: cn, Organization: p.caCert.Subject.Organization},
		NotBefore:      now.Add(-backdate),
		NotAfter:       notAfter,
		KeyUsage:       x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:       dedupe(dnsNames),
		IPAddresses:    dedupeIPs(ips),
		SubjectKeyId:   keyID(&key.PublicKey),
		AuthorityKeyId: p.caCert.SubjectKeyId,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	chainPEM := append(append([]byte{}, certPEM...), p.caPEM...)
	keyPEM := encodeKey(key)
	if err := writeFile(p.path("server.key"), keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := writeFile(p.path("server.crt"), chainPEM, 0o644); err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(chainPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	leaf, _ := x509.ParseCertificate(der)
	p.serverCert, p.serverX509 = &pair, leaf
	return leaf, nil
}

func (p *PKI) ServerX509() *x509.Certificate {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.serverX509
}

// GetCertificate is used as tls.Config.GetCertificate so a reissued certificate is picked up without restarts.
func (p *PKI) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.serverCert == nil {
		return nil, errors.New("brak certyfikatu serwera")
	}
	return p.serverCert, nil
}

// ClientSubject is the subject every client certificate gets; the CN is the TAK username.
func (p *PKI) clientSubject(username string) pkix.Name {
	org := []string{"ChameleonTAK"}
	if p.caCert != nil && len(p.caCert.Subject.Organization) > 0 {
		org = p.caCert.Subject.Organization
	}
	return pkix.Name{CommonName: username, Organization: org, OrganizationalUnit: []string{"TAK"}}
}

// IssueClient creates a key pair and certificate for a user (server-side generation for data packages).
func (p *PKI) IssueClient(username string, validity time.Duration) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, leafKeyBits)
	if err != nil {
		return nil, nil, err
	}
	cert, err := p.signClient(username, &key.PublicKey, validity)
	return cert, key, err
}

// SignCSR signs a certificate request from a client during enrollment. The subject is always
// replaced by the authenticated username, so a client cannot request someone else's identity.
func (p *PKI) SignCSR(csr *x509.CertificateRequest, username string, validity time.Duration) (*x509.Certificate, error) {
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("niepoprawny podpis CSR: %w", err)
	}
	switch k := csr.PublicKey.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			return nil, errors.New("klucz RSA w CSR jest za krótki")
		}
	default:
		// ECDSA/Ed25519 keys are accepted as-is.
	}
	return p.signClient(username, csr.PublicKey, validity)
}

func (p *PKI) signClient(username string, pub crypto.PublicKey, validity time.Duration) (*x509.Certificate, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.caCert == nil {
		return nil, ErrNoCA
	}
	now := time.Now()
	notAfter := now.Add(validity)
	if notAfter.After(p.caCert.NotAfter) {
		notAfter = p.caCert.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber:   newSerial(),
		Subject:        p.clientSubject(username),
		NotBefore:      now.Add(-clientBackdate),
		NotAfter:       notAfter,
		KeyUsage:       x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		AuthorityKeyId: p.caCert.SubjectKeyId,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, pub, p.caKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

// ClientP12 bundles a client certificate, its key and the CA chain. Legacy (3DES/SHA1) encryption
// is used because Android's keystore and older iOS releases cannot read modern PBES2 archives.
func (p *PKI) ClientP12(cert *x509.Certificate, key *rsa.PrivateKey, password string) ([]byte, error) {
	ca := p.CACert()
	if ca == nil {
		return nil, ErrNoCA
	}
	return p12Encoder.Encode(key, cert, []*x509.Certificate{ca}, password)
}

// TrustStoreP12 returns the CA certificate packed as a PKCS#12 trust store.
func (p *PKI) TrustStoreP12(password string) ([]byte, error) {
	ca := p.CACert()
	if ca == nil {
		return nil, ErrNoCA
	}
	return p12Encoder.EncodeTrustStoreEntries([]pkcs12.TrustStoreEntry{{Cert: ca, FriendlyName: "ca"}}, password)
}

// ---------- helpers ----------

func SerialHex(c *x509.Certificate) string {
	return strings.ToUpper(hex.EncodeToString(c.SerialNumber.Bytes()))
}

func CertPEM(c *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

func newSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 127)
	n, _ := rand.Int(rand.Reader, limit)
	return n.Add(n, big.NewInt(1))
}

func keyID(pub *rsa.PublicKey) []byte {
	der := x509.MarshalPKCS1PublicKey(pub)
	sum := crypto.SHA1.New()
	sum.Write(der)
	return sum.Sum(nil)
}

func encodeKey(k *rsa.PrivateKey) []byte {
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func parseCertPEM(b []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("brak bloku PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKeyPEM(b []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("brak bloku PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("klucz CA nie jest kluczem RSA")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" || seen[s] || net.ParseIP(s) != nil {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dedupeIPs(in []net.IP) []net.IP {
	seen := map[string]bool{}
	var out []net.IP
	for _, ip := range in {
		if ip == nil || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		out = append(out, ip)
	}
	return out
}

// p12Encoder matches the PKCS#12 files TAK Server produces (3DES/SHA-1, 2048 iterations), which
// Android, iOS ≤ 17 and BoringSSL all read. Truststore entries carry a friendly name because
// Android's keystore hides unnamed certificate entries.
var p12Encoder = pkcs12.LegacyDES.WithIterations(2048)

// EnrollmentP12 is the response format of the v1 signClient endpoint: a trust store whose
// entries are named signedCert, ca0, ca1… (protected with the ATAK default password).
func (p *PKI) EnrollmentP12(signed *x509.Certificate, password string) ([]byte, error) {
	ca := p.CACert()
	if ca == nil {
		return nil, ErrNoCA
	}
	return p12Encoder.EncodeTrustStoreEntries([]pkcs12.TrustStoreEntry{
		{Cert: signed, FriendlyName: "signedCert"},
		{Cert: ca, FriendlyName: "ca0"},
	}, password)
}
