// Package osc is a minimal OSC 1.0 encoder and UDP sender. The helper uses it
// to send build log lines and progress to sclang so the GUI stays responsive.
package osc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"unicode/utf8"
)

const (
	logAddress      = "/deken/log"
	progressAddress = "/deken/progress"
	maxLine         = 4000
	ellipsis        = "…"
)

// Encode builds an OSC message. Arguments may be string, int32, int or
// float32; any other type is an error.
func Encode(address string, args ...any) ([]byte, error) {
	var tags, payload bytes.Buffer
	tags.WriteByte(',')
	for _, a := range args {
		if err := encodeArg(&tags, &payload, a); err != nil {
			return nil, fmt.Errorf("encode %s: %w", address, err)
		}
	}
	var msg bytes.Buffer
	writeString(&msg, address)
	writeString(&msg, tags.String())
	msg.Write(payload.Bytes())
	return msg.Bytes(), nil
}

// encodeArg appends one argument's type tag and payload bytes.
func encodeArg(tags, payload *bytes.Buffer, a any) error {
	switch v := a.(type) {
	case string:
		tags.WriteByte('s')
		writeString(payload, v)
	case int32:
		tags.WriteByte('i')
		binary.Write(payload, binary.BigEndian, v)
	case int:
		tags.WriteByte('i')
		binary.Write(payload, binary.BigEndian, int32(v))
	case float32:
		tags.WriteByte('f')
		binary.Write(payload, binary.BigEndian, math.Float32bits(v))
	default:
		return fmt.Errorf("unsupported argument type %T", a)
	}
	return nil
}

// writeString writes s null-terminated and padded to a multiple of 4 bytes.
// It is only used on buffers whose length is already a multiple of 4 (or on
// the payload buffer, which stays aligned), so padding by length is correct.
func writeString(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteByte(0)
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

// Sender sends OSC messages to sclang over UDP on the loopback address.
type Sender struct {
	conn net.Conn // nil for the no-op sender
}

// Dial returns a Sender for 127.0.0.1:port. Port 0 returns a no-op Sender
// that sends nothing.
func Dial(port int) (*Sender, error) {
	if port == 0 {
		return &Sender{}, nil
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return &Sender{conn: conn}, nil
}

// Send encodes and sends one message. It does nothing on a no-op Sender.
func (s *Sender) Send(address string, args ...any) error {
	if s.conn == nil {
		return nil
	}
	msg, err := Encode(address, args...)
	if err != nil {
		return err
	}
	if _, err := s.conn.Write(msg); err != nil {
		return fmt.Errorf("send %s: %w", address, err)
	}
	return nil
}

// Progress sends "/deken/progress" with the job name, a fraction from 0 to 1
// and a short label.
func (s *Sender) Progress(job string, fraction float32, label string) error {
	return s.Send(progressAddress, job, fraction, label)
}

// Close releases the connection. It is a no-op for the no-op Sender.
func (s *Sender) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// logWriter turns written bytes into one "/deken/log" message per line.
type logWriter struct {
	sender  *Sender
	job     string
	partial []byte
}

// LogWriter returns a writer that sends each complete line as
// "/deken/log" job line. Lines over 4000 bytes are truncated with "…" and a
// trailing "\r" is dropped. Send errors are ignored because sclang may not be
// listening; Write always reports len(p) consumed. Call Close to send a last
// line that has no newline yet.
func LogWriter(s *Sender, job string) io.WriteCloser {
	return &logWriter{sender: s, job: job}
}

// Write buffers p and sends every complete line it now holds.
func (w *logWriter) Write(p []byte) (int, error) {
	w.partial = append(w.partial, p...)
	for {
		i := bytes.IndexByte(w.partial, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.sendLine(string(w.partial[:i]))
		w.partial = w.partial[i+1:]
	}
}

// Close sends any buffered partial line.
func (w *logWriter) Close() error {
	if len(w.partial) > 0 {
		w.sendLine(string(w.partial))
		w.partial = nil
	}
	return nil
}

// sendLine sends one cleaned-up line, ignoring send errors.
func (w *logWriter) sendLine(line string) {
	line = truncateLine(strings.TrimSuffix(line, "\r"))
	_ = w.sender.Send(logAddress, w.job, line)
}

// truncateLine cuts line to at most maxLine bytes, ending in "…", without
// splitting a UTF-8 rune.
func truncateLine(line string) string {
	if len(line) <= maxLine {
		return line
	}
	cut := maxLine - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + ellipsis
}
