// enginectl manages the signed engine pack: release tooling (keygen, pack)
// and the client-side updater (status, check, download, apply, update,
// rollback, verify). The player embeds the same internal/updater package.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"openplayer/internal/pack"
	"openplayer/internal/updater"
)

const usage = `enginectl — signed engine pack tool

Release side:
  keygen  --out PREFIX                      write PREFIX.key (secret) and PREFIX.pub
  pack    --in DIR --out DIR --spec FILE --serial N --version V --key FILE [--channel stable]

Client side (common flags: --root DIR --pubkey FILE|BASE64 --source URL|DIR):
  status                                    show installed engine
  check                                     show what an update would do
  download                                  check + stage verified files (live engine untouched)
  apply                                     swap staged update in (run before the player starts)
  update                                    check + download + apply
  rollback                                  return to the previous engine
  verify                                    re-hash installed files
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "keygen":
		err = cmdKeygen(args)
	case "pack":
		err = cmdPack(args)
	case "status", "check", "download", "apply", "update", "rollback", "verify":
		err = cmdClient(cmd, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "", "output prefix")
	fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out+".key", []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(*out+".pub", []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s.key (KEEP SECRET) and %s.pub (embed in the player)\n", *out, *out)
	return nil
}

func cmdPack(args []string) error {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	in := fs.String("in", "", "directory with engine files")
	out := fs.String("out", "", "release output directory")
	spec := fs.String("spec", "", "JSON list of components")
	serial := fs.Int64("serial", 0, "release serial (strictly increasing)")
	ver := fs.String("version", "", "engine version label")
	keyf := fs.String("key", "", "secret key file")
	channel := fs.String("channel", "stable", "release channel")
	fs.Parse(args)
	if *in == "" || *out == "" || *spec == "" || *serial <= 0 || *ver == "" || *keyf == "" {
		return fmt.Errorf("--in --out --spec --serial --version --key are required")
	}
	kb, err := os.ReadFile(*keyf)
	if err != nil {
		return err
	}
	priv, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(kb)))
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("bad secret key file")
	}
	sb, err := os.ReadFile(*spec)
	if err != nil {
		return err
	}
	var entries []pack.Entry
	if err := json.Unmarshal(sb, &entries); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	m, err := pack.Run(pack.Options{
		InDir: *in, OutDir: *out, Entries: entries, Serial: *serial,
		Channel: *channel, EngineVersion: *ver, Key: ed25519.PrivateKey(priv),
	})
	if err != nil {
		return err
	}
	fmt.Printf("packed %d components, serial %d, engine %s -> %s\n", len(m.Components), m.Serial, m.EngineVersion, *out)
	return nil
}

func loadPub(v string) (ed25519.PublicKey, error) {
	if b, err := os.ReadFile(v); err == nil {
		v = string(b)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("bad public key (give a .pub file or base64)")
	}
	return ed25519.PublicKey(raw), nil
}

func cmdClient(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := fs.String("root", "", "installation root (contains engine/)")
	pubf := fs.String("pubkey", os.Getenv("ENGINE_PUBKEY"), "public key file or base64")
	srcs := fs.String("source", os.Getenv("ENGINE_SOURCE"), "https URL or release directory")
	fs.Parse(args)
	if *root == "" || *pubf == "" {
		return fmt.Errorf("--root and --pubkey are required")
	}
	pub, err := loadPub(*pubf)
	if err != nil {
		return err
	}
	u := &updater.Updater{Root: *root, PubKey: pub}
	needSrc := cmd == "check" || cmd == "download" || cmd == "update"
	if needSrc {
		if *srcs == "" {
			return fmt.Errorf("--source is required for %s", cmd)
		}
		if u.Src, err = updater.NewSource(*srcs); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	switch cmd {
	case "status":
		if err := u.Recover(); err != nil {
			return err
		}
		m, err := u.Installed()
		if err != nil {
			return err
		}
		if m == nil {
			fmt.Println("no engine installed")
			return nil
		}
		fmt.Printf("engine %s, serial %d, channel %s, released %s\n", m.EngineVersion, m.Serial, m.Channel, m.Released)
		for _, c := range m.Components {
			fmt.Printf("  %-28s %-10s %s\n", c.Name, c.Version, c.License)
		}
	case "check", "download", "update":
		p, err := u.Check(ctx)
		if err != nil {
			return err
		}
		fmt.Print(p.Describe())
		if cmd == "check" || p.UpToDate {
			return nil
		}
		if err := u.Download(ctx, p); err != nil {
			return err
		}
		fmt.Println("downloaded and verified")
		if cmd == "update" {
			m, err := u.Apply()
			if err != nil {
				return err
			}
			fmt.Printf("applied: engine %s, serial %d\n", m.EngineVersion, m.Serial)
		}
	case "apply":
		m, err := u.Apply()
		if err != nil {
			return err
		}
		fmt.Printf("applied: engine %s, serial %d\n", m.EngineVersion, m.Serial)
	case "rollback":
		m, err := u.Rollback()
		if err != nil {
			return err
		}
		fmt.Printf("rolled back to engine %s, serial %d\n", m.EngineVersion, m.Serial)
	case "verify":
		bad, err := u.Verify()
		if err != nil {
			return err
		}
		if len(bad) > 0 {
			return fmt.Errorf("verification failed:\n  %s", strings.Join(bad, "\n  "))
		}
		fmt.Println("all installed files match the signed manifest")
	}
	return nil
}
