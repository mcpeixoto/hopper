// Package sign provides Ed25519 signing and verification for release artifacts,
// using only the standard library (no cosign/minisign dependency). The release
// workflow signs checksums.txt with a private key held in CI secrets; the updater
// verifies the signature against a public key embedded at build time before
// trusting any downloaded checksum.
package sign

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
)

// GenerateKey returns a new (publicHex, privateHex) Ed25519 keypair.
func GenerateKey() (publicHex, privateHex string, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(pub), hex.EncodeToString(priv), nil
}

// Sign returns the hex Ed25519 signature of msg using a hex-encoded private key
// (the 64-byte ed25519.PrivateKey).
func Sign(privateHex string, msg []byte) (string, error) {
	raw, err := hex.DecodeString(privateHex)
	if err != nil {
		return "", err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return "", errors.New("sign: bad private key length")
	}
	sig := ed25519.Sign(ed25519.PrivateKey(raw), msg)
	return hex.EncodeToString(sig), nil
}

// Verify reports whether sigHex is a valid Ed25519 signature of msg under the
// hex-encoded public key. A malformed key or signature returns false.
func Verify(publicHex string, msg []byte, sigHex string) bool {
	pub, err := hex.DecodeString(publicHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}
