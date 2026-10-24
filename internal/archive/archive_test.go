package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip builds a zip at path from the given headers; each entry gets body "x".
func writeZip(t *testing.T, path string, hdrs ...*zip.FileHeader) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, h := range hdrs {
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestZipUnzipRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	mustWrite(t, filepath.Join(src, "a.txt"), "alpha")
	mustWrite(t, filepath.Join(src, "sub", "deep", "b.bin"), "\x00\x01\x02")
	if err := os.MkdirAll(filepath.Join(src, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	z1, z2 := filepath.Join(tmp, "1.zip"), filepath.Join(tmp, "2.zip")
	if err := Zip(src, z1); err != nil {
		t.Fatal(err)
	}
	if err := Zip(src, z2); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(z1)
	b2, _ := os.ReadFile(z2)
	if string(b1) != string(b2) {
		t.Error("zip output is not deterministic")
	}

	out := filepath.Join(tmp, "out")
	if err := Unzip(z1, out); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"a.txt": "alpha", "sub/deep/b.bin": "\x00\x01\x02"} {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
	if fi, err := os.Stat(filepath.Join(out, "empty")); err != nil || !fi.IsDir() {
		t.Errorf("empty dir missing: %v", err)
	}
}

func TestUnzipRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	zp := filepath.Join(tmp, "evil.zip")
	writeZip(t, zp,
		&zip.FileHeader{Name: "ok.txt"},
		&zip.FileHeader{Name: "../evil.txt"})
	dst := filepath.Join(tmp, "dst")
	err := Unzip(zp, dst)
	if err == nil || !strings.Contains(err.Error(), "../evil.txt") {
		t.Fatalf("err = %v; want it to name ../evil.txt", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "evil.txt")); err == nil {
		t.Error("file escaped dstDir")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("dstDir not removed")
	}
}

func TestUnzipRejectsBadNames(t *testing.T) {
	for _, name := range []string{
		"/etc/x", "C:/x", "c:x", `a\b.txt`, "a/../../b", "a//b", "./a", ".", "a/./b",
	} {
		tmp := t.TempDir()
		zp := filepath.Join(tmp, "bad.zip")
		writeZip(t, zp, &zip.FileHeader{Name: name})
		dst := filepath.Join(tmp, "dst")
		if err := Unzip(zp, dst); err == nil {
			t.Errorf("name %q accepted", name)
		}
		if _, err := os.Stat(dst); err == nil {
			t.Errorf("name %q: dstDir left behind", name)
		}
	}
}

func TestUnzipRejectsDuplicates(t *testing.T) {
	tmp := t.TempDir()
	zp := filepath.Join(tmp, "dup.zip")
	writeZip(t, zp, &zip.FileHeader{Name: "a.txt"}, &zip.FileHeader{Name: "a.txt"})
	dst := filepath.Join(tmp, "dst")
	if err := Unzip(zp, dst); err == nil {
		t.Error("duplicate accepted")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("dstDir left behind")
	}
}

func TestUnzipRejectsSymlink(t *testing.T) {
	tmp := t.TempDir()
	zp := filepath.Join(tmp, "link.zip")
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(os.ModeSymlink | 0o777)
	writeZip(t, zp, &zip.FileHeader{Name: "ok.txt"}, h)
	dst := filepath.Join(tmp, "dst")
	if err := Unzip(zp, dst); err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("err = %v; want symlink rejection naming link", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("dstDir left behind")
	}
}

func TestUnzipExistingDir(t *testing.T) {
	tmp := t.TempDir()
	zp := filepath.Join(tmp, "ok.zip")
	writeZip(t, zp, &zip.FileHeader{Name: "new.txt"})
	dst := filepath.Join(tmp, "dst")
	mustWrite(t, filepath.Join(dst, "keep.txt"), "keep")
	if err := Unzip(zp, dst); err == nil {
		t.Fatal("existing dstDir accepted")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "keep.txt")); err != nil || string(got) != "keep" {
		t.Errorf("existing contents touched: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "new.txt")); err == nil {
		t.Error("zip content written into existing dir")
	}
}
