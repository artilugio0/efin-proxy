package certs

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

// TestGenerateRootCA tests the GenerateRootCA function
func TestGenerateRootCA(t *testing.T) {
	rootCA, rootKey, certPEM, keyPEM, err := GenerateRootCA()
	if err != nil {
		t.Fatalf("GenerateRootCA failed: %v", err)
	}

	// Check returned values are not nil
	if rootCA == nil {
		t.Error("Root CA certificate is nil")
	}
	if rootKey == nil {
		t.Error("Root CA private key is nil")
	}
	if certPEM == "" {
		t.Error("Certificate PEM is empty")
	}
	if keyPEM == "" {
		t.Error("Private key PEM is empty")
	}

	// Parse the certificate from PEM to get the actual public key
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		t.Error("Failed to decode certificate PEM")
	}
	parsedCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse generated certificate: %v", err)
	}

	// Verify certificate properties
	if !parsedCert.IsCA {
		t.Error("Root CA certificate is not marked as CA")
	}
	if parsedCert.Subject.CommonName != "Proxy Root CA" {
		t.Errorf("Expected CommonName 'Proxy Root CA', got %s", parsedCert.Subject.CommonName)
	}
	if time.Now().After(parsedCert.NotAfter) {
		t.Error("Root CA certificate is already expired")
	}

	// Verify private key matches the certificate's public key
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		t.Error("Failed to decode private key PEM")
	}
	parsedKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Errorf("Failed to parse private key: %v", err)
	}
	if parsedKey.Public().(*rsa.PublicKey).N.Cmp(parsedCert.PublicKey.(*rsa.PublicKey).N) != 0 {
		t.Error("Private key does not match certificate public key")
	}
}

// TestLoadRootCA tests the LoadRootCA function with valid and invalid files
func TestLoadRootCA(t *testing.T) {
	// Generate a valid CA to test loading
	_, rootKey, certPEM, keyPEM, err := GenerateRootCA()
	if err != nil {
		t.Fatalf("GenerateRootCA failed: %v", err)
	}

	// Parse the generated Root CA certificate for comparison
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		t.Fatalf("Failed to decode generated cert PEM")
	}
	parsedRootCA, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse generated Root CA: %v", err)
	}

	// Write temporary files
	certFile, err := ioutil.TempFile("", "test-cert-*.crt")
	if err != nil {
		t.Fatalf("Failed to create temp cert file: %v", err)
	}
	defer os.Remove(certFile.Name())
	keyFile, err := ioutil.TempFile("", "test-key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp key file: %v", err)
	}
	defer os.Remove(keyFile.Name())

	if _, err := certFile.Write([]byte(certPEM)); err != nil {
		t.Fatalf("Failed to write cert file: %v", err)
	}
	if _, err := keyFile.Write([]byte(keyPEM)); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	// Test loading valid files
	loadedCA, loadedKey, err := LoadRootCA(certFile.Name(), keyFile.Name())
	if err != nil {
		t.Fatalf("LoadRootCA failed with valid files: %v", err)
	}
	if loadedCA == nil || loadedKey == nil {
		t.Error("Loaded CA or key is nil")
	}
	if !loadedCA.Equal(parsedRootCA) {
		t.Error("Loaded CA does not match parsed generated CA")
	}
	if loadedKey.Public().(*rsa.PublicKey).N.Cmp(rootKey.Public().(*rsa.PublicKey).N) != 0 {
		t.Error("Loaded private key does not match generated private key")
	}

	// Test loading invalid files
	_, _, err = LoadRootCA("nonexistent.crt", "nonexistent.pem")
	if err == nil {
		t.Error("Expected error when loading nonexistent files, got nil")
	}

	// Test loading invalid PEM
	invalidFile, _ := ioutil.TempFile("", "test-invalid-*.txt")
	defer os.Remove(invalidFile.Name())
	invalidFile.WriteString("invalid data")
	_, _, err = LoadRootCA(invalidFile.Name(), keyFile.Name())
	if err == nil {
		t.Error("Expected error with invalid cert PEM, got nil")
	}
	_, _, err = LoadRootCA(certFile.Name(), invalidFile.Name())
	if err == nil {
		t.Error("Expected error with invalid key PEM, got nil")
	}
}

// TestGenerateCert tests the GenerateCert function
func TestGenerateCert(t *testing.T) {
	rootCA, rootKey, certPEM, _, err := GenerateRootCA()
	if err != nil {
		t.Fatalf("GenerateRootCA failed: %v", err)
	}

	// Parse the Root CA certificate for verification
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		t.Fatalf("Failed to decode Root CA cert PEM")
	}
	parsedRootCA, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse Root CA: %v", err)
	}

	host := "example.com"
	cert, err := GenerateCert([]string{host}, rootCA, rootKey)
	if err != nil {
		t.Fatalf("GenerateCert failed: %v", err)
	}
	if cert == nil {
		t.Error("Generated certificate is nil")
	}

	// Parse the certificate to verify properties
	if len(cert.Certificate) == 0 {
		t.Error("Certificate has no raw data")
	}
	parsedCert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse generated certificate: %v", err)
	}

	// Verify certificate properties
	if parsedCert.Subject.CommonName != host {
		t.Errorf("Expected CommonName %s, got %s", host, parsedCert.Subject.CommonName)
	}
	if len(parsedCert.DNSNames) != 1 || parsedCert.DNSNames[0] != host {
		t.Errorf("Expected DNSNames [%s], got %v", host, parsedCert.DNSNames)
	}
	if time.Now().After(parsedCert.NotAfter) {
		t.Error("Generated certificate is already expired")
	}

	// Verify it’s signed by the Root CA
	roots := x509.NewCertPool()
	roots.AddCert(parsedRootCA)
	opts := x509.VerifyOptions{
		Roots: roots,
	}
	if _, err := parsedCert.Verify(opts); err != nil {
		t.Errorf("Generated certificate verification failed: %v", err)
	}
}
