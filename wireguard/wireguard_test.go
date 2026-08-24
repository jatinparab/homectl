package wireguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureKeyPairRepairsEmptyKeyFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "privatekey"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "publickey"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	privateKey, publicKey, err := EnsureKeyPair(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(privateKey) == "" || strings.TrimSpace(publicKey) == "" {
		t.Fatal("EnsureKeyPair returned an empty key")
	}
	derivedPublicKey, err := PublicKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if publicKey != derivedPublicKey {
		t.Fatalf("public key %q does not match private key", publicKey)
	}
	for _, name := range []string{"privatekey", "publickey"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("%s remained empty", name)
		}
	}
}
