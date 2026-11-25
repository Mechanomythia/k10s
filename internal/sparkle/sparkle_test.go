package sparkle

import (
	"encoding/base64"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeySignVerifyAppcast(t *testing.T) {
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	k, err := ParsePrivate(priv)
	if err != nil {
		t.Fatal(err)
	}
	if PublicKey(k) != pub {
		t.Fatal("public key mismatch")
	}
	// 64-byte form accepted, tampered public half rejected.
	full := base64.StdEncoding.EncodeToString(k)
	if _, err := ParsePrivate(full); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte{}, k...)
	bad[40] ^= 1
	if _, err := ParsePrivate(base64.StdEncoding.EncodeToString(bad)); err == nil {
		t.Fatal("tampered key accepted")
	}

	f := filepath.Join(t.TempDir(), "K10s-0.2.1.zip")
	os.WriteFile(f, []byte("archive bytes"), 0o644)
	sig, n, err := SignFile(k, f)
	if err != nil || n != 13 {
		t.Fatal(err, n)
	}
	if !Verify(pub, sig, []byte("archive bytes")) || Verify(pub, sig, []byte("archive bytez")) {
		t.Fatal("verify")
	}

	x, err := Appcast("K10s", Item{Version: "0.2.1", URL: "https://github.com/a/b/releases/download/v0.2.1/K10s-0.2.1.zip?x=1&y=2",
		Length: n, EdSignature: sig, MinimumSystem: "13.0", ReleaseNotesURL: "https://github.com/a/b/releases/tag/v0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Items []struct {
			Version   string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version"`
			Enclosure struct {
				URL string `xml:"url,attr"`
				Sig string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle edSignature,attr"`
				Len int64  `xml:"length,attr"`
			} `xml:"enclosure"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(x, &doc); err != nil {
		t.Fatalf("appcast is not valid XML: %v\n%s", err, x)
	}
	it := doc.Items[0]
	if it.Version != "0.2.1" || it.Enclosure.Sig != sig || it.Enclosure.Len != 13 || !strings.Contains(it.Enclosure.URL, "&y=2") {
		t.Fatalf("%+v\n%s", it, x)
	}
}

func TestAppcastShortVersion(t *testing.T) {
	x, err := Appcast("K10s", Item{Version: "212", ShortVersion: "0.1.1", URL: "https://example.invalid/K10s.zip", Length: 1, EdSignature: "sig"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(x)
	if !strings.Contains(s, "<sparkle:version>212</sparkle:version>") || !strings.Contains(s, "<sparkle:shortVersionString>0.1.1</sparkle:shortVersionString>") {
		t.Fatalf("appcast:\n%s", s)
	}
}
