// Package sign signs and verifies the package index with minisign and manages
// the list of trusted public keys. Verification fails closed: anything that is
// not positively verified by a trusted key is rejected.
package sign

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aead.dev/minisign"
)

// errInvalid is returned whenever no trusted key verifies the signature.
var errInvalid = errors.New("index signature is not valid")

// GenerateKey creates a key pair. The public key is minisign's base64 "RW..."
// line; the private key is encrypted with password.
func GenerateKey(password string) (publicKey string, encryptedPrivateKey []byte, err error) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate signing key: %w", err)
	}
	encrypted, err := minisign.EncryptKey(password, priv)
	if err != nil {
		return "", nil, fmt.Errorf("encrypt signing key: %w", err)
	}
	return pub.String(), encrypted, nil
}

// Sign signs msg and returns the contents of a minisign signature file.
func Sign(encryptedPrivateKey []byte, password string, msg []byte) ([]byte, error) {
	priv, err := minisign.DecryptKey(password, encryptedPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("unlock signing key: %w", err)
	}
	return minisign.Sign(priv, msg), nil
}

// Verify returns nil only if sig is a valid signature of msg by at least one
// of the trusted public keys. Keys that cannot be parsed are skipped.
func Verify(msg, sig []byte, trusted []string) error {
	for _, text := range trusted {
		var key minisign.PublicKey
		if err := key.UnmarshalText([]byte(text)); err != nil {
			continue
		}
		if minisign.Verify(key, msg, sig) {
			return nil
		}
	}
	return errInvalid
}

// Trusted returns the embedded key (if non-empty) followed by the keys in the
// store file, one per line. A missing store file means no extra keys.
func Trusted(embedded, storePath string) ([]string, error) {
	var keys []string
	if embedded != "" {
		keys = append(keys, embedded)
	}
	data, err := readStore(storePath)
	if err != nil {
		return nil, err
	}
	return append(keys, parseKeys(data)...), nil
}

// readStore returns the raw store file; a missing file is empty, not an error.
func readStore(storePath string) ([]byte, error) {
	data, err := os.ReadFile(storePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read trusted keys %s: %w", storePath, err)
	}
	return data, nil
}

// parseKeys returns the key lines, skipping blanks and comments.
func parseKeys(data []byte) []string {
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "untrusted comment:") {
			continue
		}
		keys = append(keys, line)
	}
	return keys
}

// AddTrusted appends to the store file every key that is not already in it,
// creating the file and its folder if needed.
func AddTrusted(storePath string, keys []string) error {
	data, err := readStore(storePath)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, k := range parseKeys(data) {
		known[k] = true
	}
	var add strings.Builder
	if len(data) > 0 && data[len(data)-1] != '\n' {
		add.WriteString("\n") // a hand-edited file may lack a final newline
	}
	start := add.Len()
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" && !known[k] {
			known[k] = true
			add.WriteString(k + "\n")
		}
	}
	if add.Len() == start {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return fmt.Errorf("create folder for %s: %w", storePath, err)
	}
	f, err := os.OpenFile(storePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open trusted keys %s: %w", storePath, err)
	}
	if _, err := f.WriteString(add.String()); err != nil {
		f.Close()
		return fmt.Errorf("write trusted keys %s: %w", storePath, err)
	}
	return f.Close()
}
