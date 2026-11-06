package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/ThowiLabs/tavito-guard/internal/guard"
	"os"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	generate := flag.Bool("generate-key", false, "Crear clave privada Ed25519 (no publicar)")
	key := flag.String("key", "tavito-update.key", "Ruta de la clave privada")
	artifact := flag.String("artifact", "", "Binario estático a firmar")
	link := flag.String("url", "", "URL HTTPS del artefacto en releases")
	version := flag.String("version", "", "Versión semántica")
	arch := flag.String("arch", "amd64", "amd64 o arm64")
	out := flag.String("out", "manifest.json", "Archivo de manifiesto de salida")
	flag.Parse()
	if *generate {
		public, private, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		f, e := os.OpenFile(*key, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		if _, e = f.WriteString(base64.StdEncoding.EncodeToString(private)); e != nil {
			f.Close()
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
		fmt.Println("Clave pública:", base64.StdEncoding.EncodeToString(public))
		return nil
	}
	raw, e := os.ReadFile(*key)
	if e != nil {
		return e
	}
	private, e := base64.StdEncoding.DecodeString(string(raw))
	if e != nil || len(private) != ed25519.PrivateKeySize {
		return fmt.Errorf("clave privada inválida")
	}
	data, e := os.ReadFile(*artifact)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(data)
	m := guard.UpdateManifest{Version: *version, OS: "linux", Arch: *arch, URL: *link, SHA256: hex.EncodeToString(hash[:])}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, m.Payload()))
	if *arch != "amd64" && *arch != "arm64" {
		return fmt.Errorf("arquitectura no admitida")
	}
	if e = m.Verify(base64.StdEncoding.EncodeToString(ed25519.PrivateKey(private).Public().(ed25519.PublicKey))); e != nil {
		return e
	}
	return guard.AtomicJSON(*out, m)
}
