package elevate

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Debian/Ubuntu anchors directory.
const caCertsDir = "/usr/local/share/ca-certificates"

// InstallCAPath returns the path where the devctl CA PEM is stored.
func InstallCAPath() string {
	return filepath.Join(caCertsDir, CACertName)
}

// ParseCAFingerprint returns the SHA-256 hex fingerprint of a single PEM CERTIFICATE.
func ParseCAFingerprint(pemBytes []byte) (string, error) {
	block, rest := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("pem must contain exactly one CERTIFICATE block")
	}
	// Allow trailing whitespace only.
	if len(strings.TrimSpace(string(rest))) > 0 {
		if b2, _ := pem.Decode(rest); b2 != nil {
			return "", fmt.Errorf("pem must contain exactly one CERTIFICATE block")
		}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse certificate: %w", err)
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:]), nil
}

// CATrusted checks whether the installed CA file exists and matches fingerprint
// (if fingerprint is non-empty). If fingerprint is empty, existence alone is enough.
func CATrusted(wantFingerprint string) bool {
	data, err := os.ReadFile(InstallCAPath())
	if err != nil {
		return false
	}
	if wantFingerprint == "" {
		return true
	}
	got, err := ParseCAFingerprint(data)
	if err != nil {
		return false
	}
	return strings.EqualFold(got, wantFingerprint)
}

func helperInstallCA(args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return err
	}
	pemPath := flags["pem"]
	wantFP := strings.ToLower(flags["fingerprint"])
	if pemPath == "" {
		return fmt.Errorf("--pem is required")
	}
	if !filepath.IsAbs(pemPath) {
		return fmt.Errorf("--pem must be an absolute path")
	}
	// Ownership: refuse world-writable PEMs.
	st, err := os.Stat(pemPath)
	if err != nil {
		return fmt.Errorf("stat pem: %w", err)
	}
	if st.Mode().Perm()&0002 != 0 {
		return fmt.Errorf("pem path is world-writable: %s", pemPath)
	}

	pemBytes, err := os.ReadFile(pemPath)
	if err != nil {
		return fmt.Errorf("read pem: %w", err)
	}
	gotFP, err := ParseCAFingerprint(pemBytes)
	if err != nil {
		return err
	}
	if wantFP != "" && !strings.EqualFold(gotFP, wantFP) {
		return fmt.Errorf("fingerprint mismatch: got %s want %s", gotFP, wantFP)
	}

	// Ensure PEM ends with newline for update-ca-certificates.
	if !strings.HasSuffix(string(pemBytes), "\n") {
		pemBytes = append(pemBytes, '\n')
	}

	if err := writeFileAtomic(InstallCAPath(), pemBytes, 0644); err != nil {
		return fmt.Errorf("write ca cert: %w", err)
	}

	// Debian/Ubuntu.
	if _, err := os.Stat("/usr/sbin/update-ca-certificates"); err == nil {
		if err := runPinned("update-ca-certificates"); err != nil {
			return err
		}
		fmt.Println("ok")
		return nil
	}
	// RHEL/Fedora.
	if _, err := os.Stat("/usr/bin/update-ca-trust"); err == nil {
		// Copy into anchors if different layout.
		anchors := "/etc/pki/ca-trust/source/anchors/" + CACertName
		if err := writeFileAtomic(anchors, pemBytes, 0644); err != nil {
			return err
		}
		if err := runPinned("update-ca-trust", "extract"); err != nil {
			return err
		}
		fmt.Println("ok")
		return nil
	}

	fmt.Println("ok (certificate written; no update-ca-certificates tool found — may need manual trust)")
	return nil
}

func helperUninstallCA(args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return err
	}
	wantFP := strings.ToLower(flags["fingerprint"])

	path := InstallCAPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Also try RHEL path.
			path = "/etc/pki/ca-trust/source/anchors/" + CACertName
			data, err = os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("ok (not installed)")
					return nil
				}
				return err
			}
		} else {
			return err
		}
	}

	if wantFP != "" {
		got, err := ParseCAFingerprint(data)
		if err != nil {
			return fmt.Errorf("installed ca unreadable: %w — refusing to remove", err)
		}
		if !strings.EqualFold(got, wantFP) {
			return fmt.Errorf("installed ca fingerprint does not match — refusing to remove unrelated cert")
		}
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Clean alternate path too.
	_ = os.Remove("/etc/pki/ca-trust/source/anchors/" + CACertName)

	if _, err := os.Stat("/usr/sbin/update-ca-certificates"); err == nil {
		_ = runPinned("update-ca-certificates")
	} else if _, err := os.Stat("/usr/bin/update-ca-trust"); err == nil {
		_ = runPinned("update-ca-trust", "extract")
	}
	fmt.Println("ok")
	return nil
}
