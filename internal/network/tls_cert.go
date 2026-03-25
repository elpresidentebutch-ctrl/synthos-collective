package network

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// TLSConfig holds TLS certificate and key material.
type TLSConfig struct {
	Certificate *tls.Certificate
	RootCAs     *x509.CertPool
}

// CertificateManager generates and manages TLS certificates for secure peer communication.
type CertificateManager struct {
	nodeID       string
	privKey      *ecdsa.PrivateKey
	certificate  *x509.Certificate
	certPEM      []byte
	keyPEM       []byte
	rootCAs      *x509.CertPool
	cachedTLS    *tls.Certificate
}

// NewCertificateManager creates a new certificate manager.
// Generates a self-signed certificate for the node.
func NewCertificateManager(nodeID string) (*CertificateManager, error) {
	// Generate ECDSA private key (more efficient than RSA for TLS).
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	// Create self-signed certificate.
	subject := pkix.Name{
		CommonName:   nodeID,
		Organization: []string{"SYNTHOS"},
		Country:      []string{"US"},
	}

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      subject,
		Issuer:       subject,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{nodeID, "localhost"},
		IPAddresses:  nil, // Can be added if static IPs are known
	}

	// Self-sign the certificate.
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, cert, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM.
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	// Encode private key to PEM.
	privKeyBytes, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privKeyBytes,
	})

	// Create root CA pool with our certificate (for peer verification).
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(certPEM) {
		return nil, fmt.Errorf("failed to add certificate to root CAs")
	}

	// Parse certificate for caching.
	parsedCert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	cm := &CertificateManager{
		nodeID:      nodeID,
		privKey:     privKey,
		certificate: parsedCert,
		certPEM:     certPEM,
		keyPEM:      keyPEM,
		rootCAs:     rootCAs,
	}

	// Create cached TLS certificate for quick access.
	tlsCert, err := cm.createTLSCertificate()
	if err != nil {
		return nil, err
	}
	cm.cachedTLS = tlsCert

	return cm, nil
}

// createTLSCertificate creates a tls.Certificate for use in TLS connections.
func (cm *CertificateManager) createTLSCertificate() (*tls.Certificate, error) {
	tlsCert, err := tls.X509KeyPair(cm.certPEM, cm.keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS certificate pair: %w", err)
	}

	// Load the certificate chain.
	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}
	tlsCert.Leaf = cert

	return &tlsCert, nil
}

// GetTLSConfig returns a TLS configuration for the server.
func (cm *CertificateManager) GetServerTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{*cm.cachedTLS},
		MinVersion:   tls.VersionTLS13,
		// Require client certificates for mutual TLS.
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  cm.rootCAs,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
		},
	}
}

// GetClientTLSConfig returns a TLS configuration for the client.
func (cm *CertificateManager) GetClientTLSConfig(peerCertPEM []byte) *tls.Config {
	rootCAs := x509.NewCertPool()
	if peerCertPEM != nil && len(peerCertPEM) > 0 {
		rootCAs.AppendCertsFromPEM(peerCertPEM)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*cm.cachedTLS},
		RootCAs:      rootCAs,
		MinVersion:   tls.VersionTLS13,
		ServerName:   "synthos-peer", // Generic name for self-signed certs
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
		},
		// InsecureSkipVerify: false, // Always verify certificates
	}
}

// GetCertificatePEM returns the certificate in PEM format.
func (cm *CertificateManager) GetCertificatePEM() []byte {
	return cm.certPEM
}

// GetPrivateKey returns the node's private key.
func (cm *CertificateManager) GetPrivateKey() crypto.PrivateKey {
	return cm.privKey
}

// GetPublicKey returns the node's public key.
func (cm *CertificateManager) GetPublicKey() crypto.PublicKey {
	return &cm.privKey.PublicKey
}

// GetCertificate returns the x509 certificate.
func (cm *CertificateManager) GetCertificate() *x509.Certificate {
	return cm.certificate
}

// IssuePeerCertificate creates a signed certificate for a trusted peer.
// This is used for dynamic peer integration without manual certificate distribution.
func (cm *CertificateManager) IssuePeerCertificate(peerID string, peerPublicKey interface{}) (*x509.Certificate, error) {
	// Create certificate for the peer.
	subject := pkix.Name{
		CommonName:   peerID,
		Organization: []string{"SYNTHOS"},
		Country:      []string{"US"},
	}

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      subject,
		Issuer:       cm.certificate.Subject, // Signed by this node's certificate
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour), // Valid for 90 days
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{peerID, "localhost"},
	}

	// Sign with our private key.
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, cm.certificate, peerPublicKey, cm.privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to issue peer certificate: %w", err)
	}

	peerCert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse issued certificate: %w", err)
	}

	return peerCert, nil
}

// VerifyCertificateChain verifies that a peer certificate is valid and signed correctly.
func (cm *CertificateManager) VerifyCertificateChain(peerCertPEM []byte) error {
	block, _ := pem.Decode(peerCertPEM)
	if block == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}

	peerCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	opts := x509.VerifyOptions{
		Roots: cm.rootCAs,
	}

	_, err = peerCert.Verify(opts)
	if err != nil {
		return fmt.Errorf("certificate chain verification failed: %w", err)
	}

	return nil
}

// AddTrustedCertificate adds a peer certificate to the trusted pool.
func (cm *CertificateManager) AddTrustedCertificate(peerCertPEM []byte) error {
	if !cm.rootCAs.AppendCertsFromPEM(peerCertPEM) {
		return fmt.Errorf("failed to add certificate to trusted pool")
	}
	return nil
}
