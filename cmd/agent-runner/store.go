package main

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

// Task states (A2A task lifecycle subset).
const (
	StateSubmitted = "submitted"
	StateWorking   = "working"
	StateCompleted = "completed"
	StateFailed    = "failed"
	StateCanceled  = "canceled"
)

// Task is one delegated run.
type Task struct {
	ID       string    `json:"task_id"`
	Worker   string    `json:"worker"`
	Sender   string    `json:"sender"`
	Profile  Profile   `json:"profile"`
	Dir      string    `json:"dir"`
	Prompt   string    `json:"-"`
	State    string    `json:"state"`
	Result   string    `json:"result,omitempty"`
	Diff     string    `json:"diff,omitempty"`
	Error    string    `json:"error,omitempty"`
	ExitCode int       `json:"exit_code"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

func (t *Task) terminal() bool {
	return t.State == StateCompleted || t.State == StateFailed || t.State == StateCanceled
}

var errNotFound = errors.New("task not found")

// Store persists tasks in sqlite so status survives a runner restart.
type Store struct{ db *sql.DB }

func openStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY, worker TEXT, sender TEXT, profile TEXT, dir TEXT, prompt TEXT,
		state TEXT, result TEXT, diff TEXT, error TEXT, exit_code INTEGER,
		created INTEGER, updated INTEGER)`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(t *Task) error {
	_, err := s.db.Exec(`INSERT INTO tasks VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Worker, t.Sender, string(t.Profile), t.Dir, t.Prompt, t.State, t.Result, t.Diff,
		t.Error, t.ExitCode, t.Created.UnixMilli(), t.Updated.UnixMilli())
	return err
}

func (s *Store) Update(t *Task) error {
	_, err := s.db.Exec(`UPDATE tasks SET state=?, result=?, diff=?, error=?, exit_code=?, updated=? WHERE id=?`,
		t.State, t.Result, t.Diff, t.Error, t.ExitCode, t.Updated.UnixMilli(), t.ID)
	return err
}

func (s *Store) Get(id string) (*Task, error) {
	var t Task
	var profile string
	var created, updated int64
	err := s.db.QueryRow(`SELECT id, worker, sender, profile, dir, prompt, state, result, diff, error,
		exit_code, created, updated FROM tasks WHERE id=?`, id).Scan(&t.ID, &t.Worker, &t.Sender,
		&profile, &t.Dir, &t.Prompt, &t.State, &t.Result, &t.Diff, &t.Error, &t.ExitCode, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Profile = Profile(profile)
	t.Created, t.Updated = time.UnixMilli(created), time.UnixMilli(updated)
	return &t, nil
}

// FailOrphans marks tasks left non-terminal by a previous process as failed.
func (s *Store) FailOrphans(now time.Time) (int64, error) {
	res, err := s.db.Exec(`UPDATE tasks SET state=?, error=?, updated=? WHERE state IN (?,?)`,
		StateFailed, "runner restarted before completion", now.UnixMilli(), StateSubmitted, StateWorking)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
