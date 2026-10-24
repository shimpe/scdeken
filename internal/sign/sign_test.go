package sign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantInvalid = "index signature is not valid"

func newKey(t *testing.T) (string, []byte) {
	t.Helper()
	pub, priv, err := GenerateKey("pw")
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := newKey(t)
	msg := []byte(`{"schema":1}`)
	sig, err := Sign(priv, "pw", msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := Verify(msg, sig, []string{pub}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyTamperedMessage(t *testing.T) {
	pub, priv := newKey(t)
	msg := []byte(`{"schema":1}`)
	sig, _ := Sign(priv, "pw", msg)
	msg[2] ^= 1
	err := Verify(msg, sig, []string{pub})
	if err == nil || err.Error() != wantInvalid {
		t.Fatalf("want %q, got %v", wantInvalid, err)
	}
}

func TestVerifyUntrustedKey(t *testing.T) {
	_, privA := newKey(t)
	pubB, _ := newKey(t)
	msg := []byte("index")
	sig, _ := Sign(privA, "pw", msg)
	if err := Verify(msg, sig, []string{pubB}); err == nil {
		t.Fatal("signature from an untrusted key was accepted")
	}
}

func TestVerifyFailsClosed(t *testing.T) {
	pub, priv := newKey(t)
	msg := []byte("index")
	sig, _ := Sign(priv, "pw", msg)
	if err := Verify(msg, sig, nil); err == nil {
		t.Fatal("empty trusted list was accepted")
	}
	if err := Verify(msg, []byte("garbage"), []string{pub}); err == nil {
		t.Fatal("garbage signature was accepted")
	}
	if err := Verify(msg, sig, []string{"not a key", pub}); err != nil {
		t.Fatalf("unparsable key should be skipped: %v", err)
	}
	if err := Verify(msg, sig, []string{"not a key"}); err == nil || err.Error() != wantInvalid {
		t.Fatalf("want %q, got %v", wantInvalid, err)
	}
}

func TestWrongPassword(t *testing.T) {
	_, priv := newKey(t)
	if _, err := Sign(priv, "nope", []byte("index")); err == nil {
		t.Fatal("Sign succeeded with the wrong password")
	}
}

func TestTrustedStore(t *testing.T) {
	store := filepath.Join(t.TempDir(), "sub", "trusted_keys")
	if keys, err := Trusted("", store); err != nil || len(keys) != 0 {
		t.Fatalf("missing store: %v %v", keys, err)
	}
	pub, _ := newKey(t)
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
