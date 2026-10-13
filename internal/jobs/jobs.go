// Package jobs keeps long-running actions (e.g. apt upgrade) with a pollable log.
package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const maxLines = 5000

// Info is the copyable, serialisable part of a job.
type Info struct {
	ID      string `json:"id"`
	Server  string `json:"server"`
	Kind    string `json:"kind"`
	User    string `json:"user"`
	Started int64  `json:"started"`
	Ended   int64  `json:"ended,omitempty"`
	Status  string `json:"status"` // running | ok | failed
	Error   string `json:"error,omitempty"`
}

type Job struct {
	Info

	mu    sync.Mutex
	lines []string
	base  int // number of lines dropped from the front
}

type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func New() *Registry { return &Registry{jobs: map[string]*Job{}} }

// Start registers a job unless one of the same kind is already running for the server.
func (r *Registry) Start(server, kind, user string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, j := range r.jobs {
		if j.Status == "running" && j.Server == server && j.Kind == kind {
			return j, false
		}
		if j.Ended > 0 && time.Since(time.Unix(j.Ended, 0)) > time.Hour {
			delete(r.jobs, id)
		}
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	j := &Job{Info: Info{ID: hex.EncodeToString(b), Server: server, Kind: kind, User: user, Started: time.Now().Unix(), Status: "running"}}
	r.jobs[j.ID] = j
	return j, true
}

func (r *Registry) Get(id string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	return j, ok
}

func (j *Job) Append(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, line)
	if len(j.lines) > maxLines {
		drop := len(j.lines) - maxLines
		j.lines, j.base = j.lines[drop:], j.base+drop
	}
}

func (j *Job) Finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Ended = time.Now().Unix()
	if err != nil {
		j.Status, j.Error = "failed", err.Error()
	} else {
		j.Status = "ok"
	}
}

// View is a consistent snapshot of the job with log lines from offset `from`.
type View struct {
	Info
	Lines []string `json:"lines"`
	Next  int      `json:"next"`
}

func (j *Job) View(from int) View {
	j.mu.Lock()
	defer j.mu.Unlock()
	if from < j.base {
		from = j.base
	}
	v := View{Info: j.Info}
	v.Lines = append([]string(nil), j.lines[from-j.base:]...)
	v.Next = j.base + len(j.lines)
	return v
}
