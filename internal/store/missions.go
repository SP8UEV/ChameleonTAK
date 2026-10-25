package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const missionSchema = `
CREATE TABLE IF NOT EXISTS missions (
  name TEXT PRIMARY KEY COLLATE NOCASE,
  guid TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  chat_room TEXT NOT NULL DEFAULT '',
  base_layer TEXT NOT NULL DEFAULT '',
  bbox TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  classification TEXT NOT NULL DEFAULT '',
  tool TEXT NOT NULL DEFAULT 'public',
  keywords TEXT NOT NULL DEFAULT '[]',
  groups TEXT NOT NULL DEFAULT '[]',
  creator_uid TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  default_role TEXT NOT NULL DEFAULT 'MISSION_SUBSCRIBER',
  invite_only INTEGER NOT NULL DEFAULT 0,
  expiration INTEGER NOT NULL DEFAULT -1,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS mission_files (
  mission TEXT NOT NULL REFERENCES missions(name) ON DELETE CASCADE ON UPDATE CASCADE,
  hash TEXT NOT NULL,
  creator_uid TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (mission, hash)
);

CREATE TABLE IF NOT EXISTS mission_items (
  mission TEXT NOT NULL REFERENCES missions(name) ON DELETE CASCADE ON UPDATE CASCADE,
  uid TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT '',
  callsign TEXT NOT NULL DEFAULT '',
  lat REAL NOT NULL DEFAULT 0,
  lon REAL NOT NULL DEFAULT 0,
  cot TEXT NOT NULL,
  creator_uid TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (mission, uid)
);

CREATE TABLE IF NOT EXISTS mission_subs (
  mission TEXT NOT NULL REFERENCES missions(name) ON DELETE CASCADE ON UPDATE CASCADE,
  client_uid TEXT NOT NULL,
  username TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'MISSION_SUBSCRIBER',
  token TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (mission, client_uid)
);

CREATE TABLE IF NOT EXISTS mission_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mission TEXT NOT NULL REFERENCES missions(name) ON DELETE CASCADE ON UPDATE CASCADE,
  type TEXT NOT NULL,
  hash TEXT NOT NULL DEFAULT '',
  uid TEXT NOT NULL DEFAULT '',
  creator_uid TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS mission_invites (
  mission TEXT NOT NULL REFERENCES missions(name) ON DELETE CASCADE ON UPDATE CASCADE,
  client_uid TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'MISSION_SUBSCRIBER',
  creator_uid TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (mission, client_uid)
);

CREATE TABLE IF NOT EXISTS mission_logs (
  id TEXT PRIMARY KEY,
  mission TEXT NOT NULL REFERENCES missions(name) ON DELETE CASCADE ON UPDATE CASCADE,
  content TEXT NOT NULL DEFAULT '',
  creator_uid TEXT NOT NULL DEFAULT '',
  entry_uid TEXT NOT NULL DEFAULT '',
  dtg INTEGER NOT NULL DEFAULT 0,
  keywords TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL
);
`

type Mission struct {
	Name           string
	GUID           string
	Description    string
	ChatRoom       string
	BaseLayer      string
	BBox           string
	Path           string
	Classification string
	Tool           string
	Keywords       []string
	Groups         []string
	CreatorUID     string
	PasswordHash   string
	DefaultRole    string
	InviteOnly     bool
	Expiration     int64
	CreatedAt      time.Time
}

const missionCols = `name, guid, description, chat_room, base_layer, bbox, path, classification, tool, keywords, groups, creator_uid, password_hash, default_role, invite_only, expiration, created_at`

func scanMission(sc interface{ Scan(...any) error }) (*Mission, error) {
	var m Mission
	var kw, groups string
	var invite int
	var created int64
	if err := sc.Scan(&m.Name, &m.GUID, &m.Description, &m.ChatRoom, &m.BaseLayer, &m.BBox, &m.Path, &m.Classification, &m.Tool,
		&kw, &groups, &m.CreatorUID, &m.PasswordHash, &m.DefaultRole, &invite, &m.Expiration, &created); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(kw), &m.Keywords)
	_ = json.Unmarshal([]byte(groups), &m.Groups)
	m.InviteOnly, m.CreatedAt = invite == 1, ts(created)
	return &m, nil
}

func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Store) CreateMission(m *Mission) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.Tool == "" {
		m.Tool = "public"
	}
	if m.DefaultRole == "" {
		m.DefaultRole = "MISSION_SUBSCRIBER"
	}
	_, err := s.db.Exec(`INSERT INTO missions(`+missionCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.Name, m.GUID, m.Description, m.ChatRoom, m.BaseLayer, m.BBox, m.Path, m.Classification, m.Tool,
		jsonList(m.Keywords), jsonList(m.Groups), m.CreatorUID, m.PasswordHash, m.DefaultRole, b2i(m.InviteOnly), m.Expiration, unix(m.CreatedAt))
	return err
}

func (s *Store) UpdateMission(m *Mission) error {
	_, err := s.db.Exec(`UPDATE missions SET description=?, chat_room=?, base_layer=?, bbox=?, path=?, classification=?, tool=?, keywords=?, groups=?, password_hash=?, default_role=?, invite_only=?, expiration=? WHERE name=?`,
		m.Description, m.ChatRoom, m.BaseLayer, m.BBox, m.Path, m.Classification, m.Tool, jsonList(m.Keywords), jsonList(m.Groups),
		m.PasswordHash, m.DefaultRole, b2i(m.InviteOnly), m.Expiration, m.Name)
	return err
}

func (s *Store) GetMission(name string) (*Mission, error) {
	m, err := scanMission(s.db.QueryRow(`SELECT `+missionCols+` FROM missions WHERE name=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) GetMissionByGUID(guid string) (*Mission, error) {
	m, err := scanMission(s.db.QueryRow(`SELECT `+missionCols+` FROM missions WHERE guid=?`, guid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMissions() ([]*Mission, error) {
	rows, err := s.db.Query(`SELECT ` + missionCols + ` FROM missions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Mission
	for rows.Next() {
		m, err := scanMission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMission(name string) error {
	_, err := s.db.Exec(`DELETE FROM missions WHERE name=?`, name)
	return err
}

// ---------- contents ----------

type MissionFile struct {
	Hash       string
	CreatorUID string
	CreatedAt  time.Time
	File       *File
}

func (s *Store) AddMissionFile(mission, hash, creator string) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO mission_files(mission, hash, creator_uid, created_at) VALUES(?,?,?,?)`, mission, hash, creator, now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) RemoveMissionFile(mission, hash string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM mission_files WHERE mission=? AND hash=?`, mission, hash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) MissionFiles(mission string) ([]MissionFile, error) {
	rows, err := s.db.Query(`SELECT mf.hash, mf.creator_uid, mf.created_at, `+prefixed("f", fileCols)+`
		FROM mission_files mf JOIN files f ON f.hash = mf.hash WHERE mf.mission=? ORDER BY mf.created_at`, mission)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissionFile
	for rows.Next() {
		var mf MissionFile
		var created int64
		var f File
		var kw string
		var fcreated int64
		if err := rows.Scan(&mf.Hash, &mf.CreatorUID, &created, &f.Hash, &f.UID, &f.Name, &f.MIME, &f.Size, &kw, &f.CreatorUID, &f.Submitter, &f.Tool, &fcreated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(kw), &f.Keywords)
		f.CreatedAt = ts(fcreated)
		mf.CreatedAt, mf.File = ts(created), &f
		out = append(out, mf)
	}
	return out, rows.Err()
}

func prefixed(alias, cols string) string {
	var out []byte
	for i, c := range splitCols(cols) {
		if i > 0 {
			out = append(out, ", "...)
		}
		out = append(out, alias+"."+c...)
	}
	return string(out)
}

func splitCols(cols string) []string {
	var out []string
	cur := ""
	for _, r := range cols {
		switch r {
		case ',':
			out = append(out, cur)
			cur = ""
		case ' ', '\n', '\t':
		default:
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

type MissionItem struct {
	UID        string
	Type       string
	Callsign   string
	Lat, Lon   float64
	CoT        string
	CreatorUID string
	CreatedAt  time.Time
}

// UpsertMissionItem stores the latest CoT of a mission item; it reports whether the UID is new.
func (s *Store) UpsertMissionItem(mission string, it *MissionItem) (bool, error) {
	var exists int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM mission_items WHERE mission=? AND uid=?`, mission, it.UID).Scan(&exists)
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO mission_items(mission, uid, type, callsign, lat, lon, cot, creator_uid, created_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(mission, uid) DO UPDATE SET type=excluded.type, callsign=excluded.callsign, lat=excluded.lat, lon=excluded.lon, cot=excluded.cot`,
		mission, it.UID, it.Type, it.Callsign, it.Lat, it.Lon, it.CoT, it.CreatorUID, unix(it.CreatedAt))
	return exists == 0, err
}

func (s *Store) RemoveMissionItem(mission, uid string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM mission_items WHERE mission=? AND uid=?`, mission, uid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) MissionItems(mission string) ([]MissionItem, error) {
	rows, err := s.db.Query(`SELECT uid, type, callsign, lat, lon, cot, creator_uid, created_at FROM mission_items WHERE mission=? ORDER BY created_at`, mission)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissionItem
	for rows.Next() {
		var it MissionItem
		var created int64
		if err := rows.Scan(&it.UID, &it.Type, &it.Callsign, &it.Lat, &it.Lon, &it.CoT, &it.CreatorUID, &created); err != nil {
			return nil, err
		}
		it.CreatedAt = ts(created)
		out = append(out, it)
	}
	return out, rows.Err()
}

// ---------- subscriptions ----------

type MissionSub struct {
	ClientUID string
	Username  string
	Role      string
	Token     string
	CreatedAt time.Time
}

func (s *Store) Subscribe(mission string, sub *MissionSub) error {
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO mission_subs(mission, client_uid, username, role, token, created_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(mission, client_uid) DO UPDATE SET username=excluded.username, token=CASE WHEN mission_subs.token='' THEN excluded.token ELSE mission_subs.token END`,
		mission, sub.ClientUID, sub.Username, sub.Role, sub.Token, unix(sub.CreatedAt))
	return err
}

func (s *Store) Unsubscribe(mission, clientUID string) error {
	_, err := s.db.Exec(`DELETE FROM mission_subs WHERE mission=? AND client_uid=?`, mission, clientUID)
	return err
}

func (s *Store) GetSubscription(mission, clientUID string) (*MissionSub, error) {
	var sub MissionSub
	var created int64
	err := s.db.QueryRow(`SELECT client_uid, username, role, token, created_at FROM mission_subs WHERE mission=? AND client_uid=?`, mission, clientUID).
		Scan(&sub.ClientUID, &sub.Username, &sub.Role, &sub.Token, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	sub.CreatedAt = ts(created)
	return &sub, err
}

func (s *Store) Subscriptions(mission string) ([]MissionSub, error) {
	rows, err := s.db.Query(`SELECT client_uid, username, role, token, created_at FROM mission_subs WHERE mission=? ORDER BY created_at`, mission)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissionSub
	for rows.Next() {
		var sub MissionSub
		var created int64
		if err := rows.Scan(&sub.ClientUID, &sub.Username, &sub.Role, &sub.Token, &created); err != nil {
			return nil, err
		}
		sub.CreatedAt = ts(created)
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (s *Store) SetSubscriptionRole(mission, clientUID, role string) error {
	_, err := s.db.Exec(`UPDATE mission_subs SET role=? WHERE mission=? AND client_uid=?`, role, mission, clientUID)
	return err
}

// ---------- changes ----------

type MissionChange struct {
	ID         int64
	Type       string
	Hash       string
	UID        string
	CreatorUID string
	CreatedAt  time.Time
}

func (s *Store) AddMissionChange(mission string, c *MissionChange) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO mission_changes(mission, type, hash, uid, creator_uid, created_at) VALUES(?,?,?,?,?,?)`,
		mission, c.Type, c.Hash, c.UID, c.CreatorUID, unix(c.CreatedAt))
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) MissionChanges(mission string, since time.Time) ([]MissionChange, error) {
	rows, err := s.db.Query(`SELECT id, type, hash, uid, creator_uid, created_at FROM mission_changes WHERE mission=? AND created_at>=? ORDER BY id`, mission, unix(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissionChange
	for rows.Next() {
		var c MissionChange
		var created int64
		if err := rows.Scan(&c.ID, &c.Type, &c.Hash, &c.UID, &c.CreatorUID, &created); err != nil {
			return nil, err
		}
		c.CreatedAt = ts(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- invitations ----------

func (s *Store) Invite(mission, clientUID, role, creator string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO mission_invites(mission, client_uid, role, creator_uid, created_at) VALUES(?,?,?,?,?)`, mission, clientUID, role, creator, now())
	return err
}

func (s *Store) Uninvite(mission, clientUID string) error {
	_, err := s.db.Exec(`DELETE FROM mission_invites WHERE mission=? AND client_uid=?`, mission, clientUID)
	return err
}

// Invitations returns mission names a client is invited to.
func (s *Store) Invitations(clientUID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT mission FROM mission_invites WHERE client_uid=? ORDER BY created_at`, clientUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------- logs ----------

type MissionLog struct {
	ID         string
	Content    string
	CreatorUID string
	EntryUID   string
	DTG        time.Time
	Keywords   []string
	CreatedAt  time.Time
}

func (s *Store) UpsertMissionLog(mission string, l *MissionLog) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO mission_logs(id, mission, content, creator_uid, entry_uid, dtg, keywords, created_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET content=excluded.content, entry_uid=excluded.entry_uid, dtg=excluded.dtg, keywords=excluded.keywords`,
		l.ID, mission, l.Content, l.CreatorUID, l.EntryUID, unix(l.DTG), jsonList(l.Keywords), unix(l.CreatedAt))
	return err
}

func (s *Store) DeleteMissionLog(id string) error {
	_, err := s.db.Exec(`DELETE FROM mission_logs WHERE id=?`, id)
	return err
}

func (s *Store) MissionLogs(mission string) ([]MissionLog, error) {
	rows, err := s.db.Query(`SELECT id, content, creator_uid, entry_uid, dtg, keywords, created_at FROM mission_logs WHERE mission=? ORDER BY created_at`, mission)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissionLog
	for rows.Next() {
		var l MissionLog
		var dtg, created int64
		var kw string
		if err := rows.Scan(&l.ID, &l.Content, &l.CreatorUID, &l.EntryUID, &dtg, &kw, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(kw), &l.Keywords)
		l.DTG, l.CreatedAt = ts(dtg), ts(created)
		out = append(out, l)
	}
	return out, rows.Err()
}
