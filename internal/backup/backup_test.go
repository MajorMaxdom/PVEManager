package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pvemanager/internal/config"
	"pvemanager/internal/crypto"
	"pvemanager/internal/store"
)

// fixture builds an installation under a temp dir: config, key, servers, ssh key, cert and a database with data.
func fixture(t *testing.T) (Layout, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	etc, data := filepath.Join(root, "etc"), filepath.Join(root, "data")
	for _, d := range []string{etc, filepath.Join(etc, "servers.d"), filepath.Join(etc, "ssh"), data} {
		os.MkdirAll(d, 0o755)
	}
	w := func(p, c string, m os.FileMode) { os.WriteFile(p, []byte(c), m) }
	w(filepath.Join(etc, "config.yaml"), "listen: \"0.0.0.0:8443\"\n", 0o640)
	key := bytes.Repeat([]byte{9}, 32)
	os.WriteFile(filepath.Join(etc, "secret.key"), key, 0o600)
	w(filepath.Join(etc, "servers.d", "pve1.yaml"), "name: pve1\nurl: https://x\ntoken: \"t=SECRET\"\n", 0o640)
	w(filepath.Join(etc, "ssh", "id_ed25519"), "PRIVATE-KEY", 0o640)
	w(filepath.Join(etc, "ssh", "id_ed25519.pub"), "ssh-ed25519 AAAA", 0o644)
	w(filepath.Join(etc, "metrics.token"), "m-token-0123456789012345678901234", 0o640)
	w(filepath.Join(data, "tls.crt"), "CERT", 0o644)
	w(filepath.Join(data, "tls.key"), "TLSKEY", 0o600)
	cfg := config.Default()
	cfg.DataDir, cfg.SecretFile, cfg.ServersDir = data, filepath.Join(etc, "secret.key"), filepath.Join(etc, "servers.d")
	cfg.SSHKeyFile, cfg.MetricsTokenFile = filepath.Join(etc, "ssh", "id_ed25519"), filepath.Join(etc, "metrics.token")
	box, _ := crypto.New(key)
	st, err := store.Open(data, box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	_ = st.PutUser(store.User{Name: "admin", Hash: "h", Role: "admin"})
	_ = st.PutServer(store.Server{Name: "pve1", URL: "https://x", Token: "u@pve!t=SECRET"})
	return Layout{ConfigPath: filepath.Join(etc, "config.yaml"), Cfg: cfg}, st, root
}

const pass = "correct horse battery staple"

func make1(t *testing.T) ([]byte, Layout, string) {
	l, st, root := fixture(t)
	var buf bytes.Buffer
	if err := Create(&buf, pass, l, "9.9.9", st.Snapshot); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), l, root
}

func TestRoundTripRestoresEverythingOntoAFreshHost(t *testing.T) {
	arc, l, _ := make1(t)
	if bytes.Contains(arc, []byte("SECRET")) || bytes.Contains(arc, []byte("PRIVATE-KEY")) {
		t.Fatal("archive leaks plaintext")
	}
	fresh := t.TempDir()
	tg := Target{ConfigPath: "/etc/pve-manager/config.yaml", Etc: "/etc/pve-manager", DataDir: "/var/lib/pve-manager", Root: fresh}
	rep, err := Restore(bytes.NewReader(arc), pass, tg, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Manifest.Version != "9.9.9" || len(rep.Skipped) != 0 {
		t.Fatalf("report %+v", rep)
	}
	read := func(p string) string { b, _ := os.ReadFile(filepath.Join(fresh, p)); return string(b) }
	if read("etc/pve-manager/secret.key") != string(bytes.Repeat([]byte{9}, 32)) || read("etc/pve-manager/ssh/id_ed25519") != "PRIVATE-KEY" ||
		!strings.Contains(read("etc/pve-manager/servers.d/pve1.yaml"), "SECRET") || read("var/lib/pve-manager/tls.crt") != "CERT" {
		t.Fatal("files differ after restore")
	}
	if st, _ := os.Stat(filepath.Join(fresh, "etc/pve-manager/secret.key")); st.Mode().Perm() != 0o600 {
		t.Fatalf("secret.key mode %v", st.Mode())
	}
	if st, _ := os.Stat(filepath.Join(fresh, "var/lib/pve-manager/tls.key")); st.Mode().Perm() != 0o600 {
		t.Fatalf("tls.key mode %v", st.Mode())
	}
	// the restored database opens with the restored key and still holds users and the decryptable server token
	box, _ := crypto.New([]byte(read("etc/pve-manager/secret.key")))
	st, err := store.Open(filepath.Join(fresh, "var/lib/pve-manager"), box)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if u, err := st.User("admin"); err != nil || u.Role != "admin" {
		t.Fatalf("user lost: %v %v", u, err)
	}
	if sv, err := st.Servers(); err != nil || len(sv) != 1 || sv[0].Token != "u@pve!t=SECRET" {
		t.Fatalf("server token not recoverable: %v %v", sv, err)
	}
	_ = l
}

func TestWrongPassphraseTamperingAndGarbageAreRefused(t *testing.T) {
	arc, _, _ := make1(t)
	tg := Target{ConfigPath: "/c/config.yaml", Etc: "/c", DataDir: "/d", Root: t.TempDir()}
	if _, err := Restore(bytes.NewReader(arc), "another passphrase!!", tg, false, false); err != ErrBadPass {
		t.Fatalf("wrong passphrase: %v", err)
	}
	for _, i := range []int{3, len(magic) + 5, len(magic) + 20, len(arc) / 2, len(arc) - 1} { // header, salt, nonce, body, tag
		bad := append([]byte(nil), arc...)
		bad[i] ^= 0x01
		if _, err := Restore(bytes.NewReader(bad), pass, tg, false, false); err == nil {
			t.Fatalf("tampering at byte %d not detected", i)
		}
	}
	if _, err := Restore(strings.NewReader("hello"), pass, tg, false, false); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, err := Restore(bytes.NewReader(arc[:len(arc)/2]), pass, tg, false, false); err == nil {
		t.Fatal("truncated archive accepted")
	}
	if entries, _ := os.ReadDir(tg.Root); len(entries) != 0 {
		t.Fatal("a failed restore wrote files")
	}
}

func TestShortPassphraseIsRejected(t *testing.T) {
	l, st, _ := fixture(t)
	if err := Create(&bytes.Buffer{}, "short", l, "x", st.Snapshot); err == nil {
		t.Fatal("short passphrase accepted")
	}
}

func TestRestoreNeverOverwritesExistingDataWithoutForceAndDryRunWritesNothing(t *testing.T) {
	arc, _, _ := make1(t)
	root := t.TempDir()
	tg := Target{ConfigPath: "/etc/p/config.yaml", Etc: "/etc/p", DataDir: "/var/p", Root: root}
	if rep, err := Restore(bytes.NewReader(arc), pass, tg, false, true); err != nil || len(rep.Written) == 0 {
		t.Fatalf("dry run: %v %v", rep, err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("dry run wrote files")
	}
	if _, err := Restore(bytes.NewReader(arc), pass, tg, false, false); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "etc/p/secret.key"), []byte("LOCAL-KEY"), 0o600)
	if _, err := Restore(bytes.NewReader(arc), pass, tg, false, false); err == nil || !strings.Contains(err.Error(), "-force") {
		t.Fatalf("existing installation overwritten without force: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "etc/p/secret.key")); string(b) != "LOCAL-KEY" {
		t.Fatal("secret.key changed although the restore was refused")
	}
	if _, err := Restore(bytes.NewReader(arc), pass, tg, true, false); err != nil {
		t.Fatalf("force restore: %v", err)
	}
}

func TestStaleWALFilesAreRemovedOnRestore(t *testing.T) {
	arc, _, _ := make1(t)
	root := t.TempDir()
	tg := Target{ConfigPath: "/e/config.yaml", Etc: "/e", DataDir: "/d", Root: root}
	os.MkdirAll(filepath.Join(root, "d"), 0o755)
	os.WriteFile(filepath.Join(root, "d/manager.db-wal"), []byte("stale"), 0o600)
	os.WriteFile(filepath.Join(root, "d/manager.db-shm"), []byte("stale"), 0o600)
	if _, err := Restore(bytes.NewReader(arc), pass, tg, true, false); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(filepath.Join(root, "d/manager.db"+s)); err == nil {
			t.Fatalf("stale %s survived", s)
		}
	}
}

// craft builds a validly encrypted archive with arbitrary entry names, like an attacker who knows the passphrase.
func craft(t *testing.T, names ...string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: 4, Typeflag: tar.TypeReg})
		tw.Write([]byte("EVIL"))
	}
	tw.Close()
	gz.Close()
	salt, nonce := bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 12)
	blk, _ := aes.NewCipher(deriveKey(pass, salt))
	gcm, _ := cipher.NewGCM(blk)
	hdr := append(append([]byte(magic), salt...), nonce...)
	return append(append([]byte(nil), hdr...), gcm.Seal(nil, nonce, raw.Bytes(), hdr)...)
}

func TestCraftedArchiveCannotWriteOutsideTheTargetLayout(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "pvm-escape-test")
	defer os.RemoveAll(outside)
	bad := []string{"../../etc/cron.d/evil", "../pvm-escape-test", "/etc/passwd", "servers/../../escape.yaml", "servers/a/b.yaml",
		"data/../../escape.db", "ssh/../../escape", "servers/.hidden/../x.yaml", "config.yaml.evil", "secret.key/../../x", "unknown-file"}
	tg := Target{ConfigPath: "/etc/p/config.yaml", Etc: "/etc/p", DataDir: "/var/p", Root: root}
	rep, err := Restore(bytes.NewReader(craft(t, append(bad, "servers/ok.yaml")...)), pass, tg, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Written) != 1 || !strings.HasSuffix(rep.Written[0], "etc/p/servers.d/ok.yaml") {
		t.Fatalf("only the one legitimate entry may be written, got %v", rep.Written)
	}
	if len(rep.Skipped) != len(bad) {
		t.Fatalf("skipped %v", rep.Skipped)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a file was written outside the root")
	}
	var found []string
	filepath.Walk(root, func(p string, i os.FileInfo, _ error) error {
		if i != nil && !i.IsDir() {
			found = append(found, strings.TrimPrefix(p, root))
		}
		return nil
	})
	if len(found) != 1 || found[0] != "/etc/p/servers.d/ok.yaml" {
		t.Fatalf("unexpected files under the root: %v", found)
	}
}

func TestDryRunReportsConflictsInsteadOfFailing(t *testing.T) {
	arc, _, _ := make1(t)
	root := t.TempDir()
	tg := Target{ConfigPath: "/e/config.yaml", Etc: "/e", DataDir: "/d", Root: root}
	os.MkdirAll(filepath.Join(root, "e"), 0o755)
	os.WriteFile(filepath.Join(root, "e/secret.key"), []byte("LOCAL"), 0o600)
	rep, err := Restore(bytes.NewReader(arc), pass, tg, false, true)
	if err != nil {
		t.Fatalf("dry run must not fail on conflicts: %v", err)
	}
	if len(rep.Conflicts) != 1 || !strings.HasSuffix(rep.Conflicts[0], "e/secret.key") || len(rep.Written) == 0 {
		t.Fatalf("report %+v", rep)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "e/secret.key")); string(b) != "LOCAL" {
		t.Fatal("dry run changed a file")
	}
}
