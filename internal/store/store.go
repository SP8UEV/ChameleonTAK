// Package store is the SQLite persistence layer.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("nie znaleziono")

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL DEFAULT '',
  is_admin INTEGER NOT NULL DEFAULT 0,
  callsign TEXT NOT NULL DEFAULT '',
  team TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  description TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_groups (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, group_id)
);

CREATE TABLE IF NOT EXISTS devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL DEFAULT '',
  platform TEXT NOT NULL,
  client_uid TEXT NOT NULL DEFAULT '',
  last_seen INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tokens (
  token TEXT PRIMARY KEY,
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  used_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS certs (
  serial TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  cn TEXT NOT NULL,
  user_id INTEGER NOT NULL DEFAULT 0,
  device_id INTEGER NOT NULL DEFAULT 0,
  not_before INTEGER NOT NULL,
  not_after INTEGER NOT NULL,
  revoked_at INTEGER NOT NULL DEFAULT 0,
  pem TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
  hash TEXT PRIMARY KEY,
  uid TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  mime TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL,
  keywords TEXT NOT NULL DEFAULT '[]',
  creator_uid TEXT NOT NULL DEFAULT '',
  submitter TEXT NOT NULL DEFAULT '',
  tool TEXT NOT NULL DEFAULT 'public',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  time INTEGER NOT NULL,
  sender TEXT NOT NULL,
  sender_uid TEXT NOT NULL DEFAULT '',
  recipient TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS positions (
  uid TEXT PRIMARY KEY,
  data TEXT NOT NULL,
  last_seen INTEGER NOT NULL
);
`

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schemat bazy: %w", err)
	}
	if _, err := db.Exec(missionSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schemat misji: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migracja bazy: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate adds columns introduced after the first release to existing databases.
func (s *Store) migrate() error {
	cols := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		cols[name] = true
	}
	rows.Close()
	if !cols["profile_at"] {
		if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN profile_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

// DB exposes the handle for backups (VACUUM INTO).
func (s *Store) DB() *sql.DB { return s.db }

func now() int64 { return time.Now().Unix() }

func ts(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- settings ----------

func (s *Store) GetSetting(key string, v any) (bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

func (s *Store) SetSetting(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(raw))
	return err
}

// ---------- users ----------

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	Callsign     string
	Team         string
	Role         string
	Disabled     bool
	CreatedAt    time.Time
	Groups       []string
	ProfileAt    time.Time // last change of callsign/team/role (pushed to devices on connect)
}

const userCols = `id, username, password_hash, is_admin, callsign, team, role, disabled, created_at, profile_at`

func scanUser(sc interface{ Scan(...any) error }) (*User, error) {
	var u User
	var admin, disabled int
	var created, profileAt int64
	if err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &admin, &u.Callsign, &u.Team, &u.Role, &disabled, &created, &profileAt); err != nil {
		return nil, err
	}
	u.IsAdmin, u.Disabled, u.CreatedAt = admin == 1, disabled == 1, ts(created)
	u.ProfileAt = ts(profileAt)
	return &u, nil
}

func (s *Store) CreateUser(u *User) error {
	res, err := s.db.Exec(`INSERT INTO users(username,password_hash,is_admin,callsign,team,role,disabled,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		u.Username, u.PasswordHash, b2i(u.IsAdmin), u.Callsign, u.Team, u.Role, b2i(u.Disabled), now())
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	u.CreatedAt = time.Now().UTC()
	return nil
}

func (s *Store) UpdateUser(u *User) error {
	_, err := s.db.Exec(`UPDATE users SET
		profile_at = CASE WHEN callsign<>? OR team<>? OR role<>? THEN ? ELSE profile_at END,
		username=?, password_hash=?, is_admin=?, callsign=?, team=?, role=?, disabled=? WHERE id=?`,
		u.Callsign, u.Team, u.Role, now(),
		u.Username, u.PasswordHash, b2i(u.IsAdmin), u.Callsign, u.Team, u.Role, b2i(u.Disabled), u.ID)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) GetUser(id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Groups, err = s.UserGroups(u.ID)
	return u, err
}

func (s *Store) GetUserByName(name string) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Groups, err = s.UserGroups(u.ID)
	return u, err
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	memberships, err := s.allMemberships()
	if err != nil {
		return nil, err
	}
	for _, u := range out {
		u.Groups = memberships[u.ID]
	}
	return out, nil
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ---------- groups ----------

type Group struct {
	ID          int64
	Name        string
	Description string
	Members     int
	CreatedAt   time.Time
}

func (s *Store) ListGroups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT g.id, g.name, g.description, g.created_at, (SELECT COUNT(*) FROM user_groups ug WHERE ug.group_id=g.id) FROM groups g ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		var created int64
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &created, &g.Members); err != nil {
			return nil, err
		}
		g.CreatedAt = ts(created)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) GetGroup(id int64) (*Group, error) {
	var g Group
	var created int64
	err := s.db.QueryRow(`SELECT id, name, description, created_at FROM groups WHERE id=?`, id).Scan(&g.ID, &g.Name, &g.Description, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	g.CreatedAt = ts(created)
	return &g, err
}

func (s *Store) CreateGroup(g *Group) error {
	res, err := s.db.Exec(`INSERT INTO groups(name, description, created_at) VALUES(?,?,?)`, g.Name, g.Description, now())
	if err != nil {
		return err
	}
	g.ID, _ = res.LastInsertId()
	g.CreatedAt = time.Now().UTC()
	return nil
}

func (s *Store) UpdateGroup(g *Group) error {
	_, err := s.db.Exec(`UPDATE groups SET name=?, description=? WHERE id=?`, g.Name, g.Description, g.ID)
	return err
}

func (s *Store) DeleteGroup(id int64) error {
	_, err := s.db.Exec(`DELETE FROM groups WHERE id=?`, id)
	return err
}

func (s *Store) UserGroups(userID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT g.name FROM groups g JOIN user_groups ug ON ug.group_id=g.id WHERE ug.user_id=? ORDER BY g.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) allMemberships() (map[int64][]string, error) {
	rows, err := s.db.Query(`SELECT ug.user_id, g.name FROM user_groups ug JOIN groups g ON g.id=ug.group_id ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = append(out[id], n)
	}
	return out, rows.Err()
}

// SetUserGroups replaces the user's memberships, creating groups that do not exist yet.
func (s *Store) SetUserGroups(userID int64, names []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_groups WHERE user_id=?`, userID); err != nil {
		return err
	}
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO groups(name, description, created_at) VALUES(?, '', ?)`, n, now()); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO user_groups(user_id, group_id) SELECT ?, id FROM groups WHERE name=?`, userID, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- devices ----------

type Device struct {
	ID        int64
	UserID    int64
	Name      string
	Platform  string
	ClientUID string
	LastSeen  time.Time
	CreatedAt time.Time
}

const deviceCols = `id, user_id, name, platform, client_uid, last_seen, created_at`

func scanDevice(sc interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var seen, created int64
	if err := sc.Scan(&d.ID, &d.UserID, &d.Name, &d.Platform, &d.ClientUID, &seen, &created); err != nil {
		return nil, err
	}
	d.LastSeen, d.CreatedAt = ts(seen), ts(created)
	return &d, nil
}

func (s *Store) CreateDevice(d *Device) error {
	res, err := s.db.Exec(`INSERT INTO devices(user_id, name, platform, created_at) VALUES(?,?,?,?)`, d.UserID, d.Name, d.Platform, now())
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	d.CreatedAt = time.Now().UTC()
	return nil
}

func (s *Store) GetDevice(id int64) (*Device, error) {
	d, err := scanDevice(s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *Store) ListDevices(userID int64) ([]*Device, error) {
	q := `SELECT ` + deviceCols + ` FROM devices`
	var args []any
	if userID > 0 {
		q += ` WHERE user_id=?`
		args = append(args, userID)
	}
	rows, err := s.db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDevice(id int64) error {
	_, err := s.db.Exec(`DELETE FROM devices WHERE id=?`, id)
	return err
}

func (s *Store) TouchDevice(id int64, clientUID string) error {
	_, err := s.db.Exec(`UPDATE devices SET client_uid=CASE WHEN ?='' THEN client_uid ELSE ? END, last_seen=? WHERE id=?`, clientUID, clientUID, now(), id)
	return err
}

// ---------- enrollment tokens ----------

type Token struct {
	Token     string
	DeviceID  int64
	ExpiresAt time.Time
	UsedAt    time.Time
	CreatedAt time.Time
}

func (s *Store) CreateToken(t *Token) error {
	_, err := s.db.Exec(`INSERT INTO tokens(token, device_id, expires_at, created_at) VALUES(?,?,?,?)`, t.Token, t.DeviceID, unix(t.ExpiresAt), now())
	t.CreatedAt = time.Now().UTC()
	return err
}

func scanToken(sc interface{ Scan(...any) error }) (*Token, error) {
	var t Token
	var exp, used, created int64
	if err := sc.Scan(&t.Token, &t.DeviceID, &exp, &used, &created); err != nil {
		return nil, err
	}
	t.ExpiresAt, t.UsedAt, t.CreatedAt = ts(exp), ts(used), ts(created)
	return &t, nil
}

func (s *Store) GetToken(tok string) (*Token, error) {
	t, err := scanToken(s.db.QueryRow(`SELECT token, device_id, expires_at, used_at, created_at FROM tokens WHERE token=?`, tok))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// LatestToken returns the newest token of a device (or ErrNotFound).
func (s *Store) LatestToken(deviceID int64) (*Token, error) {
	t, err := scanToken(s.db.QueryRow(`SELECT token, device_id, expires_at, used_at, created_at FROM tokens WHERE device_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, deviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) MarkTokenUsed(tok string) error {
	_, err := s.db.Exec(`UPDATE tokens SET used_at=? WHERE token=? AND used_at=0`, now(), tok)
	return err
}

func (s *Store) DeleteDeviceTokens(deviceID int64) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE device_id=?`, deviceID)
	return err
}

// ---------- certificates ----------

type CertRecord struct {
	Serial    string
	Kind      string // ca | server | client
	CN        string
	UserID    int64
	DeviceID  int64
	NotBefore time.Time
	NotAfter  time.Time
	RevokedAt time.Time
	PEM       string
}

func (s *Store) AddCert(c *CertRecord) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO certs(serial, kind, cn, user_id, device_id, not_before, not_after, revoked_at, pem) VALUES(?,?,?,?,?,?,?,?,?)`,
		c.Serial, c.Kind, c.CN, c.UserID, c.DeviceID, unix(c.NotBefore), unix(c.NotAfter), unix(c.RevokedAt), c.PEM)
	return err
}

func scanCert(sc interface{ Scan(...any) error }) (*CertRecord, error) {
	var c CertRecord
	var nb, na, rv int64
	if err := sc.Scan(&c.Serial, &c.Kind, &c.CN, &c.UserID, &c.DeviceID, &nb, &na, &rv, &c.PEM); err != nil {
		return nil, err
	}
	c.NotBefore, c.NotAfter, c.RevokedAt = ts(nb), ts(na), ts(rv)
	return &c, nil
}

const certCols = `serial, kind, cn, user_id, device_id, not_before, not_after, revoked_at, pem`

func (s *Store) GetCert(serial string) (*CertRecord, error) {
	c, err := scanCert(s.db.QueryRow(`SELECT `+certCols+` FROM certs WHERE serial=?`, serial))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) ListCerts() ([]*CertRecord, error) {
	rows, err := s.db.Query(`SELECT ` + certCols + ` FROM certs ORDER BY not_before DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CertRecord
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LatestDeviceCert returns the newest non-revoked client cert of a device.
func (s *Store) LatestDeviceCert(deviceID int64) (*CertRecord, error) {
	c, err := scanCert(s.db.QueryRow(`SELECT `+certCols+` FROM certs WHERE device_id=? AND kind='client' AND revoked_at=0 ORDER BY not_before DESC LIMIT 1`, deviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) RevokeCert(serial string) error {
	_, err := s.db.Exec(`UPDATE certs SET revoked_at=? WHERE serial=? AND revoked_at=0`, now(), serial)
	return err
}

func (s *Store) RevokeUserCerts(userID int64) error {
	_, err := s.db.Exec(`UPDATE certs SET revoked_at=? WHERE user_id=? AND kind='client' AND revoked_at=0`, now(), userID)
	return err
}

func (s *Store) RevokeDeviceCerts(deviceID int64) error {
	_, err := s.db.Exec(`UPDATE certs SET revoked_at=? WHERE device_id=? AND kind='client' AND revoked_at=0`, now(), deviceID)
	return err
}

// ---------- panel sessions ----------

func (s *Store) CreateSession(token string, userID int64, ttl time.Duration) error {
	_, err := s.db.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?,?,?)`, token, userID, time.Now().Add(ttl).Unix())
	return err
}

func (s *Store) SessionUser(token string) (int64, error) {
	var id, exp int64
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token=?`, token).Scan(&id, &exp)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && exp < now()) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=? OR expires_at<?`, token, now())
	return err
}

func (s *Store) DeleteUserSessions(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

// ---------- files ----------

type File struct {
	Hash       string
	UID        string
	Name       string
	MIME       string
	Size       int64
	Keywords   []string
	CreatorUID string
	Submitter  string
	Tool       string
	CreatedAt  time.Time
}

const fileCols = `hash, uid, name, mime, size, keywords, creator_uid, submitter, tool, created_at`

func scanFile(sc interface{ Scan(...any) error }) (*File, error) {
	var f File
	var kw string
	var created int64
	if err := sc.Scan(&f.Hash, &f.UID, &f.Name, &f.MIME, &f.Size, &kw, &f.CreatorUID, &f.Submitter, &f.Tool, &created); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(kw), &f.Keywords)
	f.CreatedAt = ts(created)
	return &f, nil
}

// UpsertFile stores file metadata. Re-uploading identical content only refreshes name/keywords:
// the original uploader, UID and visibility stay (no ownership takeover by re-upload).
func (s *Store) UpsertFile(f *File) error {
	if f.Keywords == nil {
		f.Keywords = []string{}
	}
	kw, _ := json.Marshal(f.Keywords)
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO files(`+fileCols+`) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(hash) DO UPDATE SET name=excluded.name, mime=excluded.mime, keywords=excluded.keywords`,
		f.Hash, f.UID, f.Name, f.MIME, f.Size, string(kw), f.CreatorUID, f.Submitter, f.Tool, unix(f.CreatedAt))
	return err
}

func (s *Store) GetFile(hash string) (*File, error) {
	f, err := scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM files WHERE hash=?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *Store) ListFiles() ([]*File, error) {
	rows, err := s.db.Query(`SELECT ` + fileCols + ` FROM files ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) SetFileTool(hash, tool string) error {
	_, err := s.db.Exec(`UPDATE files SET tool=? WHERE hash=?`, tool, hash)
	return err
}

func (s *Store) SetFileKeywords(hash string, kw []string) error {
	raw, _ := json.Marshal(kw)
	_, err := s.db.Exec(`UPDATE files SET keywords=? WHERE hash=?`, string(raw), hash)
	return err
}

func (s *Store) DeleteFile(hash string) error {
	_, err := s.db.Exec(`DELETE FROM files WHERE hash=?`, hash)
	return err
}

// ---------- chat ----------

type ChatMessage struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	From    string    `json:"from"`
	FromUID string    `json:"fromUid"`
	To      string    `json:"to"`
	Text    string    `json:"text"`
}

func (s *Store) AddChat(m *ChatMessage) error {
	res, err := s.db.Exec(`INSERT INTO chat(time, sender, sender_uid, recipient, text) VALUES(?,?,?,?,?)`, unix(m.Time), m.From, m.FromUID, m.To, m.Text)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	_, err = s.db.Exec(`DELETE FROM chat WHERE id <= ? - 5000`, m.ID)
	return err
}

func (s *Store) ListChat(limit int) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT id, time, sender, sender_uid, recipient, text FROM (SELECT * FROM chat ORDER BY id DESC LIMIT ?) ORDER BY id`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var t int64
		if err := rows.Scan(&m.ID, &t, &m.From, &m.FromUID, &m.To, &m.Text); err != nil {
			return nil, err
		}
		m.Time = ts(t)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------- positions (last known state for the live map) ----------

func (s *Store) SavePositions(items map[string][]byte, seen map[string]time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for uid, data := range items {
		if _, err := tx.Exec(`INSERT INTO positions(uid, data, last_seen) VALUES(?,?,?) ON CONFLICT(uid) DO UPDATE SET data=excluded.data, last_seen=excluded.last_seen`,
			uid, string(data), unix(seen[uid])); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LoadPositions(since time.Time) (map[string][]byte, error) {
	rows, err := s.db.Query(`SELECT uid, data FROM positions WHERE last_seen >= ?`, unix(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var uid, data string
		if err := rows.Scan(&uid, &data); err != nil {
			return nil, err
		}
		out[uid] = []byte(data)
	}
	return out, rows.Err()
}

func (s *Store) DeletePosition(uid string) error {
	_, err := s.db.Exec(`DELETE FROM positions WHERE uid=?`, uid)
	return err
}

func (s *Store) PrunePositions(before time.Time) error {
	_, err := s.db.Exec(`DELETE FROM positions WHERE last_seen < ?`, unix(before))
	return err
}

// DeviceByClientUID returns the device that reported the given CoT UID.
func (s *Store) DeviceByClientUID(uid string) (*Device, error) {
	d, err := scanDevice(s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE client_uid=? AND client_uid<>'' LIMIT 1`, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}
