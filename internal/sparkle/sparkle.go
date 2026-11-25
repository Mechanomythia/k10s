// Package sparkle signs updates and writes the appcast for Sparkle 2, the
// macOS update framework K10s.app uses. Sparkle verifies every downloaded
// archive with an Ed25519 signature (sparkle:edSignature) against the public
// key in the app's Info.plist (SUPublicEDKey), on top of Apple's code
// signature and notarization.
//
// Key format: the private key is the base64 of the 32-byte Ed25519 seed
// (what we store as the SPARKLE_ED_PRIVATE_KEY secret). The 64-byte form
// (seed + public key) is accepted too. The public key is base64 of 32 bytes.
package sparkle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// GenerateKey returns (private seed, public key), both base64.
func GenerateKey() (priv, pub string, err error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(sk.Seed()), base64.StdEncoding.EncodeToString(pk), nil
}

// ParsePrivate decodes a base64 private key (32-byte seed or 64-byte key).
func ParsePrivate(b64 string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("sparkle private key is not base64: %w", err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		k := ed25519.NewKeyFromSeed(raw[:32])
		if !bytes.Equal(k[32:], raw[32:]) {
			return nil, errors.New("sparkle private key: public half does not match the seed")
		}
		return k, nil
	}
	return nil, fmt.Errorf("sparkle private key: want 32 or 64 bytes, got %d", len(raw))
}

// PublicKey is the SUPublicEDKey value for a private key.
func PublicKey(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

// SignFile returns the edSignature and length of an archive.
func SignFile(k ed25519.PrivateKey, path string) (sig string, length int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", 0, err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(k, data)), int64(len(data)), nil
}

// Verify checks a signature against a base64 public key.
func Verify(pubB64, sigB64 string, data []byte) bool {
	pub, err1 := base64.StdEncoding.DecodeString(pubB64)
	sig, err2 := base64.StdEncoding.DecodeString(sigB64)
	if err1 != nil || err2 != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pub, data, sig)
}

// Item is one release in the appcast.
type Item struct {
	Version         string // CFBundleVersion and CFBundleShortVersionString (e.g. 0.2.1)
	URL             string // archive download URL
	Length          int64
	EdSignature     string
	ReleaseNotesURL string
	MinimumSystem   string // e.g. 13.0
	PubDate         time.Time
}

// Appcast renders a one-item Sparkle 2 appcast (RSS 2.0).
func Appcast(title string, it Item) ([]byte, error) {
	if it.Version == "" || it.URL == "" || it.EdSignature == "" || it.Length <= 0 {
		return nil, errors.New("appcast: version, url, signature and length are required")
	}
	if it.PubDate.IsZero() {
		it.PubDate = time.Now().UTC()
	}
	esc := func(s string) string {
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">` + "\n")
	fmt.Fprintf(&b, "  <channel>\n    <title>%s</title>\n", esc(title))
	fmt.Fprintf(&b, "    <item>\n      <title>%s</title>\n", esc(title+" "+it.Version))
	fmt.Fprintf(&b, "      <pubDate>%s</pubDate>\n", it.PubDate.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "      <sparkle:version>%s</sparkle:version>\n", esc(it.Version))
	fmt.Fprintf(&b, "      <sparkle:shortVersionString>%s</sparkle:shortVersionString>\n", esc(it.Version))
	if it.MinimumSystem != "" {
		fmt.Fprintf(&b, "      <sparkle:minimumSystemVersion>%s</sparkle:minimumSystemVersion>\n", esc(it.MinimumSystem))
	}
	if it.ReleaseNotesURL != "" {
		fmt.Fprintf(&b, "      <sparkle:releaseNotesLink>%s</sparkle:releaseNotesLink>\n", esc(it.ReleaseNotesURL))
	}
	fmt.Fprintf(&b, "      <enclosure url=\"%s\" length=\"%d\" type=\"application/octet-stream\" sparkle:edSignature=\"%s\"/>\n",
		esc(it.URL), it.Length, esc(it.EdSignature))
	b.WriteString("    </item>\n  </channel>\n</rss>\n")
	return []byte(b.String()), nil
}
