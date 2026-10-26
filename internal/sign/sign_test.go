package sign

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"aead.dev/minisign"
)

const wantInvalid = "index signature is not valid"

// Encrypting or unlocking a key runs scrypt at about a second and 1 GiB of
// memory, and go test runs several test binaries at once; on a small CI
// machine that swaps for minutes. So the tests share one encrypted key and
// one signature made with it.
var (
	keyOnce sync.Once
	keyPub  string
	keyPriv []byte // encrypted with "pw"
	keyErr  error

	sigOnce sync.Once
	sigMsg  = []byte(`{"schema":1}`)
	sig     []byte
	sigErr  error
)

func testKey(t *testing.T) (string, []byte) {
	t.Helper()
	keyOnce.Do(func() { keyPub, keyPriv, keyErr = GenerateKey("pw") })
	if keyErr != nil {
		t.Fatalf("GenerateKey: %v", keyErr)
	}
	return keyPub, keyPriv
}

// testSignature is the test key's signature of sigMsg, made with Sign.
func testSignature(t *testing.T) []byte {
	t.Helper()
	_, priv := testKey(t)
	sigOnce.Do(func() { sig, sigErr = Sign(priv, "pw", sigMsg) })
	if sigErr != nil {
		t.Fatalf("Sign: %v", sigErr)
	}
	return sig
}

// publicKey is a fresh public key, made without the slow encryption, for
// tests that never sign with it.
func publicKey(t *testing.T) string {
	t.Helper()
	pub, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub.String()
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, _ := testKey(t)
	if err := Verify(sigMsg, testSignature(t), []string{pub}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestUnlockedKeySignsSeveralMessages(t *testing.T) {
	pub, priv := testKey(t)
	key, err := Unlock(priv, "pw")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	for _, msg := range []string{"index", "another index"} {
		if err := Verify([]byte(msg), key.Sign([]byte(msg)), []string{pub}); err != nil {
			t.Fatalf("Verify %q: %v", msg, err)
		}
	}
}

func TestVerifyTamperedMessage(t *testing.T) {
	pub, _ := testKey(t)
	msg := bytes.Clone(sigMsg)
	msg[2] ^= 1
	err := Verify(msg, testSignature(t), []string{pub})
	if err == nil || err.Error() != wantInvalid {
		t.Fatalf("want %q, got %v", wantInvalid, err)
	}
}

func TestVerifyUntrustedKey(t *testing.T) {
	if err := Verify(sigMsg, testSignature(t), []string{publicKey(t)}); err == nil {
		t.Fatal("signature from an untrusted key was accepted")
	}
}

func TestVerifyFailsClosed(t *testing.T) {
	pub, _ := testKey(t)
	sig := testSignature(t)
	if err := Verify(sigMsg, sig, nil); err == nil {
		t.Fatal("empty trusted list was accepted")
	}
	if err := Verify(sigMsg, []byte("garbage"), []string{pub}); err == nil {
		t.Fatal("garbage signature was accepted")
	}
	if err := Verify(sigMsg, sig, []string{"not a key", pub}); err != nil {
		t.Fatalf("unparsable key should be skipped: %v", err)
	}
	if err := Verify(sigMsg, sig, []string{"not a key"}); err == nil || err.Error() != wantInvalid {
		t.Fatalf("want %q, got %v", wantInvalid, err)
	}
}

func TestWrongPassword(t *testing.T) {
	_, priv := testKey(t)
	if _, err := Sign(priv, "nope", []byte("index")); err == nil {
		t.Fatal("Sign succeeded with the wrong password")
	}
}

func TestTrustedStore(t *testing.T) {
	store := filepath.Join(t.TempDir(), "sub", "trusted_keys")
	if keys, err := Trusted("", store); err != nil || len(keys) != 0 {
		t.Fatalf("missing store: %v %v", keys, err)
	}
	pub := publicKey(t)
	for i := 0; i < 2; i++ {
		if err := AddTrusted(store, []string{pub}); err != nil {
			t.Fatalf("AddTrusted: %v", err)
		}
	}
	data, _ := os.ReadFile(store)
	if n := strings.Count(string(data), pub); n != 1 {
		t.Fatalf("key stored %d times", n)
	}
	// Comments and blank lines are ignored; embedded key comes first.
	os.WriteFile(store, append([]byte("untrusted comment: x\n\n  "), data...), 0o600)
	keys, err := Trusted("RWEMBEDDED", store)
	if err != nil || len(keys) != 2 || keys[0] != "RWEMBEDDED" || keys[1] != pub {
		t.Fatalf("Trusted = %v, %v", keys, err)
	}
}

func TestAddTrustedWithoutTrailingNewline(t *testing.T) {
	store := filepath.Join(t.TempDir(), "trusted_keys")
	oldKey := publicKey(t)
	newK := publicKey(t)
	if err := os.WriteFile(store, []byte(oldKey), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddTrusted(store, []string{newK}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}
	keys, err := Trusted("", store)
	if err != nil || len(keys) != 2 || keys[0] != oldKey || keys[1] != newK {
		t.Fatalf("Trusted = %v, %v", keys, err)
	}
}
