package primitives

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("x509", runX509, []string{
		"cert_path", "key_path", "verify_chain", "match_hostname", "not_after_min_days",
	})
}

// runX509 validates a PEM cert: optional chain verification against the system
// trust store, optional hostname match, optional expiration cushion, and
// optional cert↔key pairing via modulus comparison (no openssl shell-out).
//
// If `cert_path` is unset the check is UNKNOWN (a YAML authoring bug); if it
// expands to an empty string the check SKIPs (the SE declined the cert prompt,
// so the input the path templates against was never supplied).
//
// `match_hostname` and `key_path` are each optional at the schema level (a YAML
// may omit the key entirely). But when the key IS present and only its value is
// empty — the cert was supplied yet its partner hostname/key was not — the
// requested verification cannot run. Rather than report a misleading PASS on a
// check titled as pairing/hostname verification, the result is an advisory WARN
// naming what could not be confirmed. A real mismatch is still a FAIL.
func runX509(rc checks.RunCtx) checks.Result {
	certPath, present := getString(rc.Check.With, "cert_path")
	if !present {
		return unknownf(rc.Check, "missing required `cert_path`")
	}
	if certPath == "" {
		return checks.Result{
			ID: rc.Check.ID, Title: rc.Check.Title,
			Status: checks.StatusSkip, Reason: "cert_path input is empty (no value supplied)",
		}
	}

	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{Path: certPath}},
	}

	leaf, intermediates, err := parseCertChain(certPath)
	if err != nil {
		r.Status = checks.StatusFail
		r.Reason = "parse cert: " + err.Error()
		return r
	}

	if v, ok := getBool(rc.Check.With, "verify_chain"); ok && v {
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		opts := x509.VerifyOptions{Roots: roots, Intermediates: intermediates}
		if _, err := leaf.Verify(opts); err != nil {
			r.Status = checks.StatusFail
			r.Reason = "chain: " + err.Error()
			return r
		}
	}

	// A verification that the YAML asked for (the key is present) but whose
	// templated input expanded empty cannot be performed. The cert may still
	// pass its chain/expiry checks, but a check titled "cert and key are
	// paired" (or "cert covers the hostname") that never compared a key or a
	// hostname must NOT report a clean PASS. Collect these gaps and surface
	// them as an advisory WARN — pev could not confirm the property, so the
	// SE should supply the missing input rather than trust a green result.
	var unverified []string

	if hn, ok := getString(rc.Check.With, "match_hostname"); ok {
		if hn == "" {
			unverified = append(unverified, "hostname coverage (no hostname supplied)")
		} else if err := leaf.VerifyHostname(hn); err != nil {
			r.Status = checks.StatusFail
			r.Reason = "hostname mismatch: " + err.Error()
			return r
		}
	}

	if days, ok := getInt(rc.Check.With, "not_after_min_days"); ok && days > 0 {
		left := time.Until(leaf.NotAfter)
		if left < time.Duration(days)*24*time.Hour {
			r.Status = checks.StatusFail
			r.Reason = fmt.Sprintf("expires in %s (< %d days)", left.Round(time.Hour), days)
			return r
		}
	}

	if keyPath, ok := getString(rc.Check.With, "key_path"); ok {
		if keyPath == "" {
			unverified = append(unverified, "cert/key pairing (no key supplied)")
		} else if err := matchCertKey(leaf, keyPath); err != nil {
			r.Status = checks.StatusFail
			r.Reason = "cert/key mismatch: " + err.Error()
			return r
		}
	}

	if len(unverified) > 0 {
		r.Status = checks.StatusWarn
		r.Reason = "cert validated, but could not verify " + strings.Join(unverified, " and ") +
			"; supply the missing input to complete the check"
		return r
	}

	r.Status = checks.StatusPass
	return r
}

// parseCertChain reads a PEM file and returns the leaf cert + a CertPool of
// intermediate CAs. Mirrors wbi/internal/ssl/verify.go ParseCertificateChain.
func parseCertChain(path string) (*x509.Certificate, *x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	pool := x509.NewCertPool()
	var leaf *x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if !strings.Contains(block.Type, "CERTIFICATE") {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case !c.IsCA:
			leaf = c
		case c.Subject.CommonName != c.Issuer.CommonName: // intermediate
			pool.AddCert(c)
		}
	}
	if leaf == nil {
		return nil, nil, fmt.Errorf("no leaf (non-CA) certificate found in %s", path)
	}
	return leaf, pool, nil
}

// matchCertKey verifies that the public key on `cert` matches the private key
// at `keyPath`. RSA only in v1; EC keys can be added when a customer needs them.
func matchCertKey(cert *x509.Certificate, keyPath string) error {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("no PEM block in %s", keyPath)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS#8. Wrap both parse errors so callers can see what failed.
		anyKey, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return fmt.Errorf("parse key: %w", errors.Join(err, err2))
		}
		k, ok := anyKey.(*rsa.PrivateKey)
		if !ok {
			return errors.New("non-RSA private key not supported in v1")
		}
		key = k
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("non-RSA public key on cert not supported in v1")
	}
	if pub.N.Cmp(key.N) != 0 || pub.E != key.E {
		return fmt.Errorf("modulus or exponent mismatch")
	}
	return nil
}
