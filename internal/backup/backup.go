// Package backup creates and restores an encrypted archive of everything pve-manager needs to be
// rebuilt on another host: database, master key, config, server definitions, SSH key and certificate.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"pvemanager/internal/config"
)

const magic = "PVMBK1\n"

// MinPassphrase is the shortest accepted passphrase: the archive holds the key that decrypts all PVE tokens.
const MinPassphrase = 12

type Manifest struct {
	Version string    `json:"version"`
	Created time.Time `json:"created"`
	Host    string    `json:"host"`
	Files   []string  `json:"files"`
}

// Layout says where things live on this host.
type Layout struct {
	ConfigPath string
	Cfg        config.Config
}

type entry struct {
	name string // logical name inside the archive
	src  string
	mode os.FileMode
}

func (l Layout) entries(dbSnapshot string) []entry {
	var e []entry
	add := func(name, src string, mode os.FileMode) {
		if src == "" {
			return
		}
		if st, err := os.Stat(src); err == nil && st.Mode().IsRegular() {
			e = append(e, entry{name, src, mode})
		}
	}
	add("config.yaml", l.ConfigPath, 0o640)
	add("secret.key", l.Cfg.SecretFile, 0o640)
	add("metrics.token", l.Cfg.MetricsTokenFile, 0o640)
	add("oidc.secret", l.Cfg.OIDC.ClientSecretFile, 0o640)
	if files, _ := filepath.Glob(filepath.Join(l.Cfg.ServersDir, "*.yaml")); files != nil {
		for _, f := range files {
			add("servers/"+filepath.Base(f), f, 0o640)
		}
	}
	if l.Cfg.SSHKeyFile != "" {
		add("ssh/id_ed25519", l.Cfg.SSHKeyFile, 0o640)
		add("ssh/id_ed25519.pub", l.Cfg.SSHKeyFile+".pub", 0o644)
	}
	add("data/tls.crt", filepath.Join(l.Cfg.DataDir, "tls.crt"), 0o644)
	add("data/tls.key", filepath.Join(l.Cfg.DataDir, "tls.key"), 0o600)
	add("data/manager.db", dbSnapshot, 0o640)
	return e
}

func deriveKey(pass string, salt []byte) []byte {
	return argon2.IDKey([]byte(pass), salt, 3, 64*1024, 2, 32)
}

// Create writes the encrypted archive to out. snapshotDB must write a consistent database copy to the given path.
func Create(out io.Writer, pass string, l Layout, version string, snapshotDB func(dest string) error) error {
	if len(pass) < MinPassphrase {
		return fmt.Errorf("the passphrase must have at least %d characters", MinPassphrase)
	}
	tmp, err := os.MkdirTemp("", "pvmbk-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	db := filepath.Join(tmp, "manager.db")
	if err := snapshotDB(db); err != nil {
		return fmt.Errorf("database snapshot: %w", err)
	}
	ents := l.entries(db)

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	host, _ := os.Hostname()
	man := Manifest{Version: version, Created: time.Now().UTC(), Host: host}
	for _, e := range ents {
		man.Files = append(man.Files, e.name)
	}
	mb, _ := json.MarshalIndent(man, "", "  ")
	put := func(name string, mode os.FileMode, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(mode), Size: int64(len(data)), ModTime: man.Created, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	if err := put("manifest.json", 0o644, mb); err != nil {
		return err
	}
	for _, e := range ents {
		data, err := os.ReadFile(e.src)
		if err != nil {
			return fmt.Errorf("%s: %w", e.src, err)
		}
		if err := put(e.name, e.mode, data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}

	salt, nonce := make([]byte, 16), make([]byte, 12)
	_, _ = rand.Read(salt)
	_, _ = rand.Read(nonce)
	blk, _ := aes.NewCipher(deriveKey(pass, salt))
	gcm, _ := cipher.NewGCM(blk)
	header := append([]byte(magic), salt...)
	header = append(header, nonce...)
	sealed := gcm.Seal(nil, nonce, raw.Bytes(), header) // header is authenticated too
	if _, err := out.Write(header); err != nil {
		return err
	}
	_, err = out.Write(sealed)
	return err
}

var (
	reServerFile = regexp.MustCompile(`^servers/[A-Za-z0-9._-]+\.yaml$`)
	ErrBadPass   = errors.New("wrong passphrase or damaged backup")
)

// Target maps logical archive names to paths on the restoring host. Paths come from the LOCAL layout,
// never from the archive, so a crafted archive cannot write elsewhere.
type Target struct {
	ConfigPath string
	Etc        string // directory of config.yaml, e.g. /etc/pve-manager
	DataDir    string
	Root       string // optional prefix, for dry runs and tests
}

func DefaultTarget(configPath string) Target {
	d := config.Default()
	return Target{ConfigPath: configPath, Etc: filepath.Dir(configPath), DataDir: d.DataDir}
}

func (t Target) path(name string) (string, bool) {
	var p string
	switch {
	case name == "config.yaml":
		p = t.ConfigPath
	case name == "secret.key", name == "metrics.token", name == "oidc.secret":
		p = filepath.Join(t.Etc, name)
	case reServerFile.MatchString(name):
		p = filepath.Join(t.Etc, "servers.d", path.Base(name))
	case name == "ssh/id_ed25519" || name == "ssh/id_ed25519.pub":
		p = filepath.Join(t.Etc, name)
	case name == "data/tls.crt" || name == "data/tls.key" || name == "data/manager.db":
		p = filepath.Join(t.DataDir, path.Base(name))
	default:
		return "", false
	}
	return filepath.Join(t.Root, p), true
}

func isData(name string) bool { return strings.HasPrefix(name, "data/") }

// Open decrypts and unpacks the archive into memory (it is small).
func Open(in io.Reader, pass string) (Manifest, map[string][]byte, error) {
	var man Manifest
	all, err := io.ReadAll(io.LimitReader(in, 512<<20))
	if err != nil {
		return man, nil, err
	}
	if len(all) < len(magic)+16+12+16 || string(all[:len(magic)]) != magic {
		return man, nil, errors.New("this is not a pve-manager backup")
	}
	hdr := all[:len(magic)+16+12]
	salt, nonce := hdr[len(magic):len(magic)+16], hdr[len(magic)+16:]
	blk, _ := aes.NewCipher(deriveKey(pass, salt))
	gcm, _ := cipher.NewGCM(blk)
	plain, err := gcm.Open(nil, nonce, all[len(hdr):], hdr)
	if err != nil {
		return man, nil, ErrBadPass
	}
	gz, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return man, nil, err
	}
	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return man, nil, err
		}
		data, err := io.ReadAll(io.LimitReader(tr, 256<<20))
		if err != nil {
			return man, nil, err
		}
		if h.Name == "manifest.json" {
			if err := json.Unmarshal(data, &man); err != nil {
				return man, nil, err
			}
			continue
		}
		files[h.Name] = data
	}
	return man, files, nil
}

type Report struct {
	Manifest  Manifest
	Written   []string
	DataFile  map[string]bool // written paths that belong to the data directory
	Conflicts []string        // existing files a real restore would refuse to replace without force
	Skipped   []string
}

// Restore writes the files of the archive to the target layout. Existing state is only replaced with force.
func Restore(in io.Reader, pass string, t Target, force, dryRun bool) (Report, error) {
	rep := Report{DataFile: map[string]bool{}}
	man, files, err := Open(in, pass)
	if err != nil {
		return rep, err
	}
	rep.Manifest = man
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	type job struct {
		name, dst string
		data      []byte
	}
	var jobs []job
	for _, n := range names {
		dst, ok := t.path(n)
		if !ok {
			rep.Skipped = append(rep.Skipped, n) // unknown entries are ignored, never written
			continue
		}
		jobs = append(jobs, job{n, dst, files[n]})
	}
	if !force {
		for _, j := range jobs {
			if j.name == "data/manager.db" || j.name == "secret.key" {
				if _, err := os.Stat(j.dst); err == nil {
					if !dryRun {
						return rep, fmt.Errorf("%s already exists; stop the service and use -force to replace this installation's data", j.dst)
					}
					rep.Conflicts = append(rep.Conflicts, j.dst) // a dry run reports instead of failing
				}
			}
		}
	}
	for _, j := range jobs {
		rep.Written = append(rep.Written, j.dst)
		rep.DataFile[j.dst] = isData(j.name)
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(j.dst), 0o750); err != nil {
			return rep, err
		}
		mode := os.FileMode(0o640)
		if strings.HasSuffix(j.name, ".key") && strings.HasPrefix(j.name, "data/") || j.name == "secret.key" {
			mode = 0o600
		}
		if strings.HasSuffix(j.name, ".pub") || j.name == "data/tls.crt" {
			mode = 0o644
		}
		if j.name == "data/manager.db" { // stale WAL files of an older database would corrupt the restored one
			_ = os.Remove(j.dst + "-wal")
			_ = os.Remove(j.dst + "-shm")
		}
		if err := writeAtomic(j.dst, j.data, mode); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func writeAtomic(dst string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".pvmrestore-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// FixOwnership gives the files to the service account like install.sh does: configuration root:<group>,
// data <user>:<group>. It is a no-op when not running as root or when the account does not exist.
func FixOwnership(rep Report, svcUser string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	u, err := user.Lookup(svcUser)
	if err != nil {
		return nil
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	for _, p := range rep.Written {
		owner := 0
		if rep.DataFile[p] {
			owner = uid
		}
		if err := os.Chown(p, owner, gid); err != nil {
			return err
		}
	}
	return nil
}
