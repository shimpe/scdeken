package osc

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestEncodeExactBytes(t *testing.T) {
	got, err := Encode("/a", "x", int32(1))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("/a\x00\x00,si\x00x\x00\x00\x00\x00\x00\x00\x01")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeRejectsType(t *testing.T) {
	if _, err := Encode("/a", 1.5); err == nil {
		t.Fatal("expected error for float64")
	}
}

func TestEncodeIntAndFloat(t *testing.T) {
	got, err := Encode("/a", 2, float32(1))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("/a\x00\x00,if\x00\x00\x00\x00\x02\x3f\x80\x00\x00")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// listen starts a local UDP listener and returns it with its port.
func listen(t *testing.T) (net.PacketConn, int) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc, pc.LocalAddr().(*net.UDPAddr).Port
}

// read returns the next datagram or fails after a short wait.
func read(t *testing.T, pc net.PacketConn) []byte {
	t.Helper()
	buf := make([]byte, 8192)
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return buf[:n]
}

func TestSenderDelivers(t *testing.T) {
	pc, port := listen(t)
	s, err := Dial(port)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Send("/hello", "w", int32(7)); err != nil {
		t.Fatal(err)
	}
	want, _ := Encode("/hello", "w", int32(7))
	if got := read(t, pc); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNoopSender(t *testing.T) {
	s, err := Dial(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send("/a", "x"); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress("j", 0.5, "half"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProgress(t *testing.T) {
	pc, port := listen(t)
	s, _ := Dial(port)
	defer s.Close()
	if err := s.Progress("job1", 0.5, "building"); err != nil {
		t.Fatal(err)
	}
	want, _ := Encode("/deken/progress", "job1", float32(0.5), "building")
	if got := read(t, pc); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLogWriterSplitsLines(t *testing.T) {
	pc, port := listen(t)
	s, _ := Dial(port)
	defer s.Close()
	w := LogWriter(s, "job1")
	if n, err := w.Write([]byte("a\r\nb\nc")); err != nil || n != 6 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	for _, line := range []string{"a", "b"} {
		want, _ := Encode("/deken/log", "job1", line)
		if got := read(t, pc); !bytes.Equal(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	w.Close()
	want, _ := Encode("/deken/log", "job1", "c")
	if got := read(t, pc); !bytes.Equal(got, want) {
		t.Fatalf("after Close: got %q, want %q", got, want)
	}
}

func TestTruncateLine(t *testing.T) {
	long := strings.Repeat("é", 3000) // 6000 bytes, 2-byte runes
	got := truncateLine(long)
	if len(got) > maxLine || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
		t.Fatalf("bad truncation: len=%d valid=%v", len(got), utf8.ValidString(got))
	}
	if short := "short"; truncateLine(short) != short {
		t.Fatal("short line changed")
	}
}
