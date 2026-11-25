// Command k10s-release is release tooling for the macOS build: Sparkle
// auto-update signing. Used by macos/K10s/build-app.sh and CI.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/mechanomythia/k10s/internal/sparkle"
)

const usage = `k10s-release <command>   release tooling (Sparkle auto-update signing)

  sparkle-pubkey                 print SUPublicEDKey for $SPARKLE_ED_PRIVATE_KEY
  appcast --version V --url U --file F [--notes URL] [--out appcast.xml]
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "k10s-release:", err)
		os.Exit(1)
	}
}

func privateKey() (string, error) {
	v := os.Getenv("SPARKLE_ED_PRIVATE_KEY")
	if v == "" {
		return "", errors.New("SPARKLE_ED_PRIVATE_KEY is not set")
	}
	return v, nil
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "sparkle-pubkey":
		v, err := privateKey()
		if err != nil {
			return err
		}
		k, err := sparkle.ParsePrivate(v)
		if err != nil {
			return err
		}
		fmt.Println(sparkle.PublicKey(k))
		return nil
	case "appcast":
		fs := flag.NewFlagSet("appcast", flag.ExitOnError)
		version := fs.String("version", "", "version (CFBundleVersion)")
		url := fs.String("url", "", "download URL of the archive")
		file := fs.String("file", "", "the archive (signed and measured)")
		notes := fs.String("notes", "", "release notes URL")
		out := fs.String("out", "appcast.xml", "output file")
		_ = fs.Parse(args[1:])
		v, err := privateKey()
		if err != nil {
			return err
		}
		k, err := sparkle.ParsePrivate(v)
		if err != nil {
			return err
		}
		sig, n, err := sparkle.SignFile(k, *file)
		if err != nil {
			return err
		}
		x, err := sparkle.Appcast("K10s", sparkle.Item{Version: *version, URL: *url, Length: n, EdSignature: sig,
			ReleaseNotesURL: *notes, MinimumSystem: "14.0"})
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, x, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s (version %s, %d bytes signed)\n", *out, *version, n)
		return nil
	}
	fmt.Print(usage)
	return fmt.Errorf("unknown command %q", args[0])
}
