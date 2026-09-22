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
	nodeID      string
	privKey     *ecdsa.PrivateKey
	certificate *x509.Certificate
	certPEM     []byte
	keyPEM      []byte
	rootCAs     *x509.CertPool
	cachedTLS   *tls.Certificate
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

// GetServerTLSConfig returns a TLS configuration for the server.
//
// This used to set ClientAuth: RequireAndVerifyClientCert with ClientCAs:
// cm.rootCAs -- but cm.rootCAs contains only THIS node's own self-signed
// certificate (see NewCertificateManager above), and every node generates
// its own independent self-signed cert. That combination would never
// actually let two different nodes complete a TLS handshake at all: a
// real peer's client certificate is signed by ITS OWN key, which is never
// in this node's rootCAs, so RequireAndVerifyClientCert would reject every
// real peer unconditionally. This was already broken as written, before
// anything used it.
//
// Real peer identity for this transport doesn't come from TLS certificates
// at all -- there's no CA infrastructure here, just per-node self-signed
// certs regenerated on every process start. It comes from the ed25519
// handshake layered on top (see peer_auth.go), cryptographically bound to
// this specific TLS session via channel binding (see secure_transport.go's
// channelBinding helper and CreateHandshake/VerifyHandshake's doc
// comments) so that binding can't be satisfied by relaying a handshake
// from a different TLS session. TLS's job here is solely to encrypt the
// wire; ClientAuth is left at its default (no client certificate
// requested at all) rather than asking for one we have no way to check.
func (cm *CertificateManager) GetServerTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{*cm.cachedTLS},
		MinVersion:   tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
		},
	}
}

// GetClientTLSConfig returns a TLS configuration for the client.
//
// InsecureSkipVerify is deliberate, not an oversight: there is no real CA
// here (see GetServerTLSConfig's doc comment above for why the old
// RequireAndVerifyClientCert/ClientCAs approach could never have worked),
// so Go's default certificate-chain verification has nothing valid to
// check a self-signed peer certificate against -- it would reject every
// real peer's cert unconditionally, the same way the old server config
// did. Skipping it does not skip peer authentication: TLS here provides
// encryption only, and the channel-bound ed25519 handshake immediately
// following the TLS handshake (see peer_auth.go and secure_transport.go)
// provides authentication, cryptographically tied to this exact TLS
// session so it can't be satisfied by an attacker relaying a handshake
// captured on a different one.
func (cm *CertificateManager) GetClientTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{*cm.cachedTLS},
		MinVersion:         tls.VersionTLS13,
		ServerName:         "synthos-peer", // Generic name for self-signed certs
		InsecureSkipVerify: true,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
		},
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

// IssuePeerCertificate, VerifyCertificateChain, and AddTrustedCertificate
// below are not called anywhere in this codebase today -- this transport's
// actual trust model doesn't use a certificate chain at all (see
// GetServerTLSConfig/GetClientTLSConfig's doc comments above). They're
// left in place as building blocks for a real CA-based PKI, should this
// project ever want node certificates issued/verified through an actual
// chain of trust instead of the current TLS-for-encryption +
// channel-bound-ed25519-handshake-for-authentication design.

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
