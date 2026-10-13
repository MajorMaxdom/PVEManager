// Package store persists users, servers, sessions and the audit log in SQLite.
package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"pvemanager/internal/crypto"
)

type Store struct {
	db  *sql.DB
	box *crypto.Box
}

const schema = `
CREATE TABLE IF NOT EXISTS users(
  name TEXT PRIMARY KEY, hash TEXT NOT NULL, role TEXT NOT NULL CHECK(role IN('admin','operator','viewer')),
  created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS servers(
  name TEXT PRIMARY KEY, url TEXT NOT NULL, node TEXT, fingerprint TEXT, token BLOB NOT NULL,
  ssh_user TEXT, ssh_host TEXT, ssh_cmd TEXT, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(
  id TEXT PRIMARY KEY, user TEXT NOT NULL REFERENCES users(name) ON DELETE CASCADE,
  expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS audit(
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, user TEXT NOT NULL,
  action TEXT NOT NULL, target TEXT, detail TEXT, ok INTEGER NOT NULL);
`

func Open(dataDir string, box *crypto.Box) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "manager.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if _, err := db.Exec(keySchema); err != nil {
		return nil, err
	}
	if _, err := db.Exec(extraSchema); err != nil {
		return nil, err
	}
	// migrations: add columns introduced after v0.1 (ignore "duplicate column")
	if _, err := db.Exec(`ALTER TABLE servers ADD COLUMN ssh_hostkey TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return nil, err
	}
	return &Store{db: db, box: box}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Snapshot writes a consistent copy of the database to dest (which must not exist), also while the service runs.
func (s *Store) Snapshot(dest string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, dest)
	return err
}

// ---- users

type User struct{ Name, Hash, Role string }

var ErrNotFound = errors.New("not found")

func (s *Store) PutUser(u User) error {
	_, err := s.db.Exec(`INSERT INTO users(name,hash,role,created) VALUES(?,?,?,?)
	  ON CONFLICT(name) DO UPDATE SET hash=excluded.hash, role=excluded.role`, u.Name, u.Hash, u.Role, time.Now().Unix())
	return err
}

func (s *Store) User(name string) (User, error) {
	var u User
	err := s.db.QueryRow(`SELECT name,hash,role FROM users WHERE name=?`, name).Scan(&u.Name, &u.Hash, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) Users() ([]User, error) {
	rows, err := s.db.Query(`SELECT name,hash,role FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Name, &u.Hash, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CountUsers() (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return
}

// ---- servers

type Server struct {
	Name, URL, Node, Fingerprint, Token  string // Token is plaintext in memory only
	SSHUser, SSHHost, SSHCmd, SSHHostKey string
}

func (s *Store) PutServer(v Server) error {
	sealed, err := s.box.Seal([]byte(v.Token))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO servers(name,url,node,fingerprint,token,ssh_user,ssh_host,ssh_cmd,ssh_hostkey,created)
	  VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(name) DO UPDATE SET url=excluded.url,node=excluded.node,
	  fingerprint=excluded.fingerprint,token=excluded.token,ssh_user=excluded.ssh_user,
	  ssh_host=excluded.ssh_host,ssh_cmd=excluded.ssh_cmd,ssh_hostkey=excluded.ssh_hostkey`,
		v.Name, v.URL, v.Node, v.Fingerprint, sealed, v.SSHUser, v.SSHHost, v.SSHCmd, v.SSHHostKey, time.Now().Unix())
	return err
}

func (s *Store) DeleteServer(name string) error {
	_, err := s.db.Exec(`DELETE FROM servers WHERE name=?`, name)
	return err
}

func (s *Store) Servers() ([]Server, error) {
	rows, err := s.db.Query(`SELECT name,url,COALESCE(node,''),COALESCE(fingerprint,''),token,
	  COALESCE(ssh_user,''),COALESCE(ssh_host,''),COALESCE(ssh_cmd,''),COALESCE(ssh_hostkey,'') FROM servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	for rows.Next() {
		var v Server
		var sealed []byte
		if err := rows.Scan(&v.Name, &v.URL, &v.Node, &v.Fingerprint, &sealed, &v.SSHUser, &v.SSHHost, &v.SSHCmd, &v.SSHHostKey); err != nil {
			return nil, err
		}
		plain, err := s.box.Open(sealed)
		if err != nil {
			return nil, err
		}
		v.Token = string(plain)
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- sessions

func (s *Store) PutSession(id, user string, expires time.Time) error {
	_, err := s.db.Exec(`INSERT INTO sessions(id,user,expires) VALUES(?,?,?)`, id, user, expires.Unix())
	return err
}

// Session returns the user for a live session id (id is a hash of the cookie value).
func (s *Store) Session(id string) (User, error) {
	var u User
	err := s.db.QueryRow(`SELECT u.name,u.hash,u.role FROM sessions s JOIN users u ON u.name=s.user
	  WHERE s.id=? AND s.expires>?`, id, time.Now().Unix()).Scan(&u.Name, &u.Hash, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

func (s *Store) PurgeSessions() {
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires<?`, time.Now().Unix())
}

// ---- audit

type AuditEntry struct {
	TS     int64  `json:"ts"`
	User   string `json:"user"`
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail"`
	OK     bool   `json:"ok"`
}

func (s *Store) Audit(user, action, target, detail string, ok bool) {
	_, _ = s.db.Exec(`INSERT INTO audit(ts,user,action,target,detail,ok) VALUES(?,?,?,?,?,?)`,
		time.Now().Unix(), user, action, target, detail, ok)
}

func (s *Store) AuditList(limit int) ([]AuditEntry, error) {
	rows, err := s.db.Query(`SELECT ts,user,action,COALESCE(target,''),COALESCE(detail,''),ok FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.TS, &e.User, &e.Action, &e.Target, &e.Detail, &e.OK); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
