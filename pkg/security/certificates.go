package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
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
)

type Certificate struct {
	Name       string    `json:"name"`
	Subject    string    `json:"subject"`
	DNSNames   []string  `json:"dnsNames"`
	NotBefore  time.Time `json:"notBefore"`
	NotAfter   time.Time `json:"notAfter"`
	DaysLeft   int       `json:"daysLeft"`
	Expiring   bool      `json:"expiring"`
	HasKey     bool      `json:"hasKey"`
	SelfSigned bool      `json:"selfSigned"`
}

type CertificateManager struct {
	Dir string
	Now func() time.Time
	mu  sync.Mutex
}

func NewCertificateManager(dir string) *CertificateManager {
	return &CertificateManager{Dir: dir, Now: time.Now}
}

func (m *CertificateManager) Paths(name string) (string, string, error) {
	name, err := safeName(name)
	if err != nil {
		return "", "", err
	}
	certPath, keyPath := filepath.Join(m.Dir, name, "certificate.crt"), filepath.Join(m.Dir, name, "private.key")
	if !fileExists(certPath) || !fileExists(keyPath) {
		certPath, keyPath = filepath.Join(m.Dir, name+".crt"), filepath.Join(m.Dir, name+".key")
	}
	if !fileExists(certPath) || !fileExists(keyPath) {
		return "", "", errors.New("bound certificate or private key is missing")
	}
	return certPath, keyPath, nil
}

func (m *CertificateManager) List() []Certificate {
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return nil
	}
	var result []Certificate
	for _, entry := range entries {
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".certificate-") {
				continue
			}
			name := entry.Name()
			b, err := os.ReadFile(filepath.Join(m.Dir, name, "certificate.crt"))
			if err != nil {
				continue
			}
			cert, err := parseCertificate(b)
			if err == nil {
				result = append(result, m.describe(name, cert))
			}
			continue
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".crt" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".crt")
		b, err := os.ReadFile(filepath.Join(m.Dir, entry.Name()))
		if err != nil {
			continue
		}
		cert, err := parseCertificate(b)
		if err != nil {
			continue
		}
		result = append(result, m.describe(name, cert))
	}
	return result
}

func (m *CertificateManager) Upload(name string, certPEM, keyPEM []byte) (Certificate, error) {
	info, err := m.Validate(name, certPEM, keyPEM)
	if err != nil {
		return Certificate{}, err
	}
	name = info.Name
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return Certificate{}, err
	}
	target := filepath.Join(m.Dir, name)
	if _, err := os.Stat(target); err == nil {
		return Certificate{}, errors.New("certificate already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Certificate{}, err
	}
	tmp, err := os.MkdirTemp(m.Dir, ".certificate-"+name+"-")
	if err != nil {
		return Certificate{}, err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "certificate.crt"), certPEM, 0644); err != nil {
		return Certificate{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "private.key"), keyPEM, 0600); err != nil {
		return Certificate{}, err
	}
	if err := os.Rename(tmp, target); err != nil {
		return Certificate{}, err
	}
	info.HasKey = true
	return info, nil
}

func (m *CertificateManager) Validate(name string, certPEM, keyPEM []byte) (Certificate, error) {
	name, err := safeName(name)
	if err != nil {
		return Certificate{}, err
	}
	cert, err := parseCertificate(certPEM)
	if err != nil {
		return Certificate{}, err
	}
	key, err := parsePrivateKey(keyPEM)
	if err != nil {
		return Certificate{}, err
	}
	if !publicKeysEqual(cert.PublicKey, key.Public()) {
		return Certificate{}, errors.New("certificate and private key do not match")
	}
	now := m.Now()
	if now.Before(cert.NotBefore) {
		return Certificate{}, errors.New("certificate is not valid yet")
	}
	if !now.Before(cert.NotAfter) {
		return Certificate{}, errors.New("certificate has expired")
	}
	return m.describe(name, cert), nil
}

func (m *CertificateManager) SelfSigned(name string, hosts []string, validDays int) (Certificate, error) {
	if err := m.ValidateSelfSigned(name, hosts, validDays); err != nil {
		return Certificate{}, err
	}
	name, _ = safeName(name)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return Certificate{}, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return Certificate{}, err
	}
	now := m.Now()
	tpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: hosts[0]}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(time.Duration(validDays) * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else if strings.TrimSpace(host) != "" {
			tpl.DNSNames = append(tpl.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return m.Upload(name, certPEM, keyPEM)
}

func (m *CertificateManager) ValidateSelfSigned(name string, hosts []string, validDays int) error {
	if _, err := safeName(name); err != nil {
		return err
	}
	if validDays < 1 || validDays > 3650 {
		return errors.New("validDays must be between 1 and 3650")
	}
	if len(hosts) == 0 {
		return errors.New("at least one host is required")
	}
	for _, host := range hosts {
		if strings.TrimSpace(host) == "" {
			return errors.New("certificate hosts cannot be empty")
		}
	}
	return nil
}

func (m *CertificateManager) describe(name string, cert *x509.Certificate) Certificate {
	days := int(cert.NotAfter.Sub(m.Now()).Hours() / 24)
	_, _, pathErr := m.Paths(name)
	return Certificate{Name: name, Subject: cert.Subject.String(), DNSNames: cert.DNSNames, NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, DaysLeft: days, Expiring: days < 30, HasKey: pathErr == nil, SelfSigned: cert.Subject.String() == cert.Issuer.String() && cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil}
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}
func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid PEM private key")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("unsupported private key")
	}
	k, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key must be RSA")
	}
	return k, nil
}
func publicKeysEqual(a any, b any) bool {
	aa, err := x509.MarshalPKIXPublicKey(a)
	if err != nil {
		return false
	}
	bb, err := x509.MarshalPKIXPublicKey(b)
	return err == nil && string(aa) == string(bb)
}
func safeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", errors.New("invalid certificate name")
	}
	return name, nil
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
