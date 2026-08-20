package transport

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	cert1, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert failed: %v", err)
	}
	leaf1, err := x509.ParseCertificate(cert1.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	now := time.Now()
	if leaf1.SerialNumber.Sign() <= 0 {
		t.Fatal("serial number must be positive")
	}
	skew := now.Sub(leaf1.NotBefore)
	if skew < 4*time.Minute || skew > 10*time.Minute {
		t.Errorf("NotBefore %v should be ~5 minutes in the past (measured skew %v)", leaf1.NotBefore, skew)
	}
	if !leaf1.NotAfter.After(now) {
		t.Errorf("NotAfter %v must be in the future", leaf1.NotAfter)
	}

	cert2, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert failed: %v", err)
	}
	leaf2, err := x509.ParseCertificate(cert2.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}
	if leaf1.SerialNumber.Cmp(leaf2.SerialNumber) == 0 {
		t.Fatal("serial numbers must be unique per issued certificate")
	}
}
