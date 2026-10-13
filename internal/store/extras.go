package store

import (
	"encoding/json"
	"strings"
	"time"
)

const extraSchema = `
CREATE TABLE IF NOT EXISTS channels(
  id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, kind TEXT NOT NULL,
  config BLOB NOT NULL, events TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS schedules(
  id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, cron TEXT NOT NULL, action TEXT NOT NULL,
  server TEXT NOT NULL, gtype TEXT NOT NULL DEFAULT '', vmid INTEGER NOT NULL DEFAULT 0, params TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1, last_run INTEGER NOT NULL DEFAULT 0, last_status TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL);
`

// ---- notification channels

type Channel struct {
	ID      int64             `json:"id"`
	Name    string            `json:"name"`
	Kind    string            `json:"kind"`
	Config  map[string]string `json:"-"` // secrets: never serialised to the browser
	Events  []string          `json:"events"`
	Enabled bool              `json:"enabled"`
}

func (s *Store) PutChannel(c Channel) (int64, error) {
	raw, _ := json.Marshal(c.Config)
	sealed, err := s.box.Seal(raw)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO channels(name,kind,config,events,enabled,created) VALUES(?,?,?,?,?,?)`,
		c.Name, c.Kind, sealed, strings.Join(c.Events, ","), b2i(c.Enabled), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) Channels() ([]Channel, error) {
	rows, err := s.db.Query(`SELECT id,name,kind,config,events,enabled FROM channels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		var c Channel
		var sealed []byte
		var ev string
		var en int
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &sealed, &ev, &en); err != nil {
			return nil, err
		}
		plain, err := s.box.Open(sealed)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(plain, &c.Config)
		c.Events = []string{}
		if ev != "" {
			c.Events = strings.Split(ev, ",")
		}
		c.Enabled = en == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteChannel(id int64) error {
	_, err := s.db.Exec(`DELETE FROM channels WHERE id=?`, id)
	return err
}

// ---- schedules

type Schedule struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Cron       string            `json:"cron"`
	Action     string            `json:"action"` // start|shutdown|reboot|backup|apt-refresh
	Server     string            `json:"server"`
	GType      string            `json:"gtype"`
	VMID       int               `json:"vmid"`
	Params     map[string]string `json:"params"`
	Enabled    bool              `json:"enabled"`
	LastRun    int64             `json:"lastRun"`
	LastStatus string            `json:"lastStatus"`
}

func (s *Store) PutSchedule(c Schedule) (int64, error) {
	p, _ := json.Marshal(c.Params)
	res, err := s.db.Exec(`INSERT INTO schedules(name,cron,action,server,gtype,vmid,params,enabled,created) VALUES(?,?,?,?,?,?,?,?,?)`,
		c.Name, c.Cron, c.Action, c.Server, c.GType, c.VMID, string(p), b2i(c.Enabled), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) Schedules() ([]Schedule, error) {
	rows, err := s.db.Query(`SELECT id,name,cron,action,server,gtype,vmid,params,enabled,last_run,last_status FROM schedules ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		var c Schedule
		var p string
		var en int
		if err := rows.Scan(&c.ID, &c.Name, &c.Cron, &c.Action, &c.Server, &c.GType, &c.VMID, &p, &en, &c.LastRun, &c.LastStatus); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(p), &c.Params)
		c.Enabled = en == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SetScheduleEnabled(id int64, on bool) error {
	r, err := s.db.Exec(`UPDATE schedules SET enabled=? WHERE id=?`, b2i(on), id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSchedule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM schedules WHERE id=?`, id)
	return err
}

func (s *Store) MarkScheduleRun(id int64, status string) {
	_, _ = s.db.Exec(`UPDATE schedules SET last_run=?, last_status=? WHERE id=?`, time.Now().Unix(), status, id)
}

// ---- user management

func (s *Store) DeleteUser(name string) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE name=?`, name) // sessions go with it (ON DELETE CASCADE)
	return err
}

func (s *Store) CountAdmins() (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&n)
	return
}

// DeleteUserSessions ends every session of a user except the one with id keep (empty keeps none).
func (s *Store) DeleteUserSessions(name, keep string) {
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE user=? AND id<>?`, name, keep)
}

// ---- API keys (bearer tokens for scripts)

const keySchema = `
CREATE TABLE IF NOT EXISTS api_keys(
  id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, hash TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL CHECK(role IN('admin','operator','viewer')), prefix TEXT NOT NULL,
  created INTEGER NOT NULL, last_used INTEGER NOT NULL DEFAULT 0, created_by TEXT NOT NULL);
`

type APIKey struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Prefix    string `json:"prefix"` // first characters, to recognise a key; never the secret
	Created   int64  `json:"created"`
	LastUsed  int64  `json:"lastUsed"`
	CreatedBy string `json:"createdBy"`
}

func (s *Store) PutAPIKey(name, hash, role, prefix, by string) (int64, error) {
	r, err := s.db.Exec(`INSERT INTO api_keys(name,hash,role,prefix,created,created_by) VALUES(?,?,?,?,?,?)`,
		name, hash, role, prefix, time.Now().Unix(), by)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) APIKeys() ([]APIKey, error) {
	rows, err := s.db.Query(`SELECT id,name,role,prefix,created,last_used,created_by FROM api_keys ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Role, &k.Prefix, &k.Created, &k.LastUsed, &k.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// APIKeyByHash returns the key for a token hash and records its use (at most once a minute).
func (s *Store) APIKeyByHash(hash string) (APIKey, error) {
	var k APIKey
	err := s.db.QueryRow(`SELECT id,name,role,prefix,created,last_used,created_by FROM api_keys WHERE hash=?`, hash).
		Scan(&k.ID, &k.Name, &k.Role, &k.Prefix, &k.Created, &k.LastUsed, &k.CreatedBy)
	if err != nil {
		return k, ErrNotFound
	}
	if now := time.Now().Unix(); now-k.LastUsed >= 60 {
		_, _ = s.db.Exec(`UPDATE api_keys SET last_used=? WHERE id=?`, now, k.ID)
	}
	return k, nil
}

func (s *Store) DeleteAPIKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM api_keys WHERE id=?`, id)
	return err
}
