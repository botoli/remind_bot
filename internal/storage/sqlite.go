package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }
type Intake struct {
	ID           int64
	ReminderID   string
	ScheduledAt  time.Time
	ConfirmedAt  *time.Time
	TakenAt      *time.Time
	Status       string
	InitialSent  bool
	SnoozedUntil *time.Time
	SnoozeCount  int
	FollowupSent bool
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY,value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS intake_log (id INTEGER PRIMARY KEY AUTOINCREMENT,reminder_id TEXT NOT NULL,scheduled_at TEXT NOT NULL,confirmed_at TEXT,status TEXT NOT NULL DEFAULT 'pending',initial_sent INTEGER NOT NULL DEFAULT 0,snoozed_until TEXT,snooze_count INTEGER NOT NULL DEFAULT 0,followup_sent INTEGER NOT NULL DEFAULT 0,UNIQUE(reminder_id,scheduled_at))`,
	} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	// Existing databases predate the separate time reported by the user.
	var columnCount int
	if err = db.QueryRow(`SELECT count(*) FROM pragma_table_info('intake_log') WHERE name='taken_at'`).Scan(&columnCount); err != nil {
		db.Close()
		return nil, err
	}
	if columnCount == 0 {
		if _, err = db.Exec(`ALTER TABLE intake_log ADD COLUMN taken_at TEXT`); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) ChatID() (int64, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key='chat_id'`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}
func (s *Store) SetChatID(id int64) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES('chat_id',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(id))
	return err
}
func (s *Store) Ensure(id string, at time.Time) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO intake_log(reminder_id,scheduled_at) VALUES(?,?)`, id, at.Format(time.RFC3339))
	return err
}
func (s *Store) Get(id int64) (Intake, error) {
	return scan(s.DB.QueryRow(`SELECT id,reminder_id,scheduled_at,confirmed_at,taken_at,status,initial_sent,snoozed_until,snooze_count,followup_sent FROM intake_log WHERE id=?`, id))
}
func (s *Store) ByOccurrence(reminderID string, at time.Time) (Intake, error) {
	return scan(s.DB.QueryRow(`SELECT id,reminder_id,scheduled_at,confirmed_at,taken_at,status,initial_sent,snoozed_until,snooze_count,followup_sent FROM intake_log WHERE reminder_id=? AND scheduled_at=?`, reminderID, at.Format(time.RFC3339)))
}
func scan(row *sql.Row) (Intake, error) {
	var i Intake
	var scheduled string
	var confirmed, taken, snoozed sql.NullString
	var sent, follow int
	err := row.Scan(&i.ID, &i.ReminderID, &scheduled, &confirmed, &taken, &i.Status, &sent, &snoozed, &i.SnoozeCount, &follow)
	if err != nil {
		return i, err
	}
	i.ScheduledAt, err = time.Parse(time.RFC3339, scheduled)
	if err != nil {
		return i, err
	}
	if confirmed.Valid {
		v, e := time.Parse(time.RFC3339, confirmed.String)
		if e != nil {
			return i, e
		}
		i.ConfirmedAt = &v
	}
	if taken.Valid {
		v, e := time.Parse(time.RFC3339, taken.String)
		if e != nil {
			return i, e
		}
		i.TakenAt = &v
	}
	if snoozed.Valid {
		v, e := time.Parse(time.RFC3339, snoozed.String)
		if e != nil {
			return i, e
		}
		i.SnoozedUntil = &v
	}
	i.InitialSent = sent != 0
	i.FollowupSent = follow != 0
	return i, nil
}
func (s *Store) MarkSent(id int64) error {
	_, e := s.DB.Exec(`UPDATE intake_log SET initial_sent=1 WHERE id=?`, id)
	return e
}
func (s *Store) MarkTaken(id int64, now time.Time) (bool, error) {
	r, e := s.DB.Exec(`UPDATE intake_log SET status='taken',confirmed_at=?,taken_at=?,snoozed_until=NULL WHERE id=? AND status!='taken'`, now.Format(time.RFC3339), now.Format(time.RFC3339), id)
	if e != nil {
		return false, e
	}
	n, e := r.RowsAffected()
	return n > 0, e
}
func (s *Store) RecordTaken(id int64, takenAt, confirmedAt time.Time) (bool, error) {
	r, e := s.DB.Exec(`UPDATE intake_log SET status='taken',confirmed_at=?,taken_at=?,snoozed_until=NULL WHERE id=? AND status!='taken'`, confirmedAt.Format(time.RFC3339), takenAt.Format(time.RFC3339), id)
	if e != nil {
		return false, e
	}
	n, e := r.RowsAffected()
	return n > 0, e
}
func (s *Store) CorrectTakenTime(id int64, takenAt time.Time) (bool, error) {
	r, e := s.DB.Exec(`UPDATE intake_log SET taken_at=? WHERE id=? AND status='taken'`, takenAt.Format(time.RFC3339), id)
	if e != nil {
		return false, e
	}
	n, e := r.RowsAffected()
	return n > 0, e
}
func (s *Store) Snooze(id int64, until time.Time, max int) (bool, error) {
	r, e := s.DB.Exec(`UPDATE intake_log SET snoozed_until=?,snooze_count=snooze_count+1 WHERE id=? AND status='pending' AND snoozed_until IS NULL AND snooze_count<?`, until.Format(time.RFC3339), id, max)
	if e != nil {
		return false, e
	}
	n, e := r.RowsAffected()
	return n > 0, e
}
func (s *Store) ClearSnooze(id int64) error {
	_, e := s.DB.Exec(`UPDATE intake_log SET snoozed_until=NULL WHERE id=?`, id)
	return e
}
func (s *Store) MarkFollowup(id int64) error {
	_, e := s.DB.Exec(`UPDATE intake_log SET followup_sent=1,status='missed' WHERE id=? AND status='pending'`, id)
	return e
}
func (s *Store) List(from, to time.Time) ([]Intake, error) {
	rows, e := s.DB.Query(`SELECT id,reminder_id,scheduled_at,confirmed_at,taken_at,status,initial_sent,snoozed_until,snooze_count,followup_sent FROM intake_log WHERE scheduled_at>=? AND scheduled_at<? ORDER BY scheduled_at`, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Intake
	for rows.Next() {
		var i Intake
		var at string
		var confirmed, taken, snoozed sql.NullString
		var sent, follow int
		if e = rows.Scan(&i.ID, &i.ReminderID, &at, &confirmed, &taken, &i.Status, &sent, &snoozed, &i.SnoozeCount, &follow); e != nil {
			return nil, e
		}
		i.ScheduledAt, e = time.Parse(time.RFC3339, at)
		if e != nil {
			return nil, e
		}
		if confirmed.Valid {
			v, err := time.Parse(time.RFC3339, confirmed.String)
			if err != nil {
				return nil, err
			}
			i.ConfirmedAt = &v
		}
		if taken.Valid {
			v, err := time.Parse(time.RFC3339, taken.String)
			if err != nil {
				return nil, err
			}
			i.TakenAt = &v
		}
		if snoozed.Valid {
			v, err := time.Parse(time.RFC3339, snoozed.String)
			if err != nil {
				return nil, err
			}
			i.SnoozedUntil = &v
		}
		i.InitialSent = sent != 0
		i.FollowupSent = follow != 0
		out = append(out, i)
	}
	return out, rows.Err()
}
