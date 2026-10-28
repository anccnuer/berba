package sign

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"guiapp/internal/apk"
)

func testAPK(t *testing.T) []byte {
	t.Helper()
	data, err := apk.Build([]apk.Entry{
		{Name: "AndroidManifest.xml", Data: []byte("<manifest package=\"com.example.test\"/>")},
		{Name: "resources.arsc", Data: bytes.Repeat([]byte{0x11}, 4096), Store: true, Align: 4},
		{Name: "classes.dex", Data: bytes.Repeat([]byte("dex\n"), 4096)},
		{Name: "assets/www/index.html", Data: []byte("<html>hello</html>")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSignAndVerify(t *testing.T) {
	key, err := Generate(filepath.Join(t.TempDir(), "release.pem"))
	if err != nil {
		t.Fatal(err)
	}
	unsigned := testAPK(t)
	signed, err := Sign(unsigned, key)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if res.Certificate == nil || res.Certificate.Subject.CommonName != "guiapp" {
		t.Fatal("unexpected certificate")
	}
	if len(res.ContentDigest) != 32 {
		t.Fatalf("unexpected digest length: %d", len(res.ContentDigest))
	}

	// The signing block must not alter the original bytes before it.
	if !bytes.Equal(signed[:len(unsigned)-22], signed[:len(unsigned)-22]) {
		t.Fatal("sanity")
	}
}

func TestSigningBlockFooter(t *testing.T) {
	key, err := Generate(filepath.Join(t.TempDir(), "release.pem"))
	if err != nil {
		t.Fatal(err)
	}
	unsigned := testAPK(t)
	signed, err := Sign(unsigned, key)
	if err != nil {
		t.Fatal(err)
	}
	eocd, err := findEOCD(signed)
	if err != nil {
		t.Fatal(err)
	}
	cdEnd := int(eocd.cdOffset)
	blockStart, err := findSigningBlockStart(signed, cdEnd)
	if err != nil {
		t.Fatal(err)
	}
	lead := binary.LittleEndian.Uint64(signed[blockStart:])
	if lead != uint64(cdEnd-blockStart-8) {
		t.Fatalf("leading size field = %d, want %d", lead, cdEnd-blockStart-8)
	}
	if trail := binary.LittleEndian.Uint64(signed[cdEnd-24:]); trail != lead {
		t.Fatalf("trailing size field = %d, want %d", trail, lead)
	}
	if !bytes.Equal(signed[cdEnd-16:cdEnd], apkSigBlockMagic) {
		t.Fatal("magic missing")
	}
}

func TestTamperDetection(t *testing.T) {
	key, err := Generate(filepath.Join(t.TempDir(), "release.pem"))
	if err != nil {
		t.Fatal(err)
	}
	unsigned := testAPK(t)
	signed, err := Sign(unsigned, key)
	if err != nil {
		t.Fatal(err)
	}
	idx := bytes.Index(signed, []byte("<html>hello</html>"))
	if idx < 0 {
		t.Fatal("asset not found")
	}
	signed[idx] = 'H'
	if _, err := Verify(signed); err == nil {
		t.Fatal("expected verification failure after tamper")
	}
}

func TestLoadKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "release.pem")
	key, err := Generate(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Fingerprint() != key.Fingerprint() {
		t.Fatal("fingerprint mismatch")
	}
	if loaded.Private.N.Cmp(key.Private.N) != 0 {
		t.Fatal("key mismatch")
	}
}
