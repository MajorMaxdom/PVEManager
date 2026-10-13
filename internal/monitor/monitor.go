// Package monitor turns poller state into notifications and runs scheduled actions.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"pvemanager/internal/cron"
	"pvemanager/internal/notify"
	"pvemanager/internal/poller"
	"pvemanager/internal/store"
)

type Monitor struct {
	St   *store.Store
	Poll *poller.Poller
	Log  *slog.Logger

	mu        sync.Mutex
	fails     map[string]int
	offline   map[string]bool
	diskHigh  map[string]bool
	seenTasks map[string]bool
	seeded    map[string]bool
	updCount  map[string]int
	cronCache map[string]*cron.Spec
}

func New(st *store.Store, p *poller.Poller, log *slog.Logger) *Monitor {
	return &Monitor{St: st, Poll: p, Log: log, fails: map[string]int{}, offline: map[string]bool{}, diskHigh: map[string]bool{},
		seenTasks: map[string]bool{}, seeded: map[string]bool{}, updCount: map[string]int{}, cronCache: map[string]*cron.Spec{}}
}

// Emit delivers an event to every enabled channel subscribed to it.
func (m *Monitor) Emit(event, title, body string) {
	chans, err := m.St.Channels()
	if err != nil {
		m.Log.Warn("notify: cannot load channels", "err", err)
		return
	}
	for _, ch := range chans {
		if !ch.Enabled || !contains(ch.Events, event) {
			continue
		}
		go func(ch store.Channel) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := notify.Send(ctx, ch.Kind, ch.Config, title, body)
			m.St.Audit("system", "notify."+event, ch.Name, title, err == nil)
			if err != nil {
				m.Log.Warn("notify failed", "channel", ch.Name, "event", event, "err", err)
			}
		}(ch)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Run drives the periodic checks until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	fast := time.NewTicker(15 * time.Second)
	defer fast.Stop()
	var lastMinute, lastTasks, lastUpdates time.Time
	time.Sleep(20 * time.Second) // let the poller fill its first snapshot
	for {
		now := time.Now()
		m.checkServers()
		if now.Sub(lastTasks) >= time.Minute {
			lastTasks = now
			m.checkTasks(ctx)
		}
		if now.Sub(lastUpdates) >= time.Hour {
			lastUpdates = now
			m.checkUpdates(ctx)
		}
		if mnt := now.Truncate(time.Minute); mnt.After(lastMinute) {
			lastMinute = mnt
			m.runSchedules(ctx, mnt)
		}
		select {
		case <-ctx.Done():
			return
		case <-fast.C:
		}
	}
}

func (m *Monitor) checkServers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.Poll.Snapshot() {
		if s.Updated == 0 {
			continue // not polled yet (just started): neither online nor offline
		}
		if !s.Online {
			m.fails[s.Name]++
			if m.fails[s.Name] >= 2 && !m.offline[s.Name] {
				m.offline[s.Name] = true
				m.Emit("server.offline", s.Name+" is unreachable", s.Name+": "+s.Error)
			}
			continue
		}
		m.fails[s.Name] = 0
		if m.offline[s.Name] {
			m.offline[s.Name] = false
			m.Emit("server.online", s.Name+" is back online", s.Name+" answers again.")
		}
		for _, n := range s.Nodes {
			if n.MaxDisk == 0 {
				continue
			}
			key, frac := s.Name+"/"+n.Node, float64(n.Disk)/float64(n.MaxDisk)
			switch {
			case frac >= 0.90 && !m.diskHigh[key]:
				m.diskHigh[key] = true
				m.Emit("node.disk", fmt.Sprintf("%s: root disk %.0f%% full", key, frac*100), fmt.Sprintf("%s has used %.0f%% of its root filesystem.", key, frac*100))
			case frac < 0.85:
				m.diskHigh[key] = false
			}
		}
	}
}

func (m *Monitor) checkTasks(ctx context.Context) {
	for _, s := range m.Poll.Snapshot() {
		if !s.Online {
			continue
		}
		c, _, ok := m.Poll.Client(s.Name)
		if !ok {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		tasks, err := c.ClusterTasks(cctx)
		cancel()
		if err != nil {
			continue
		}
		first := !m.seeded[s.Name]
		m.seeded[s.Name] = true // the first pass only records history: no alerts for old failures
		for _, t := range tasks {
			if t.EndTime == 0 || m.seenTasks[t.UPID] {
				continue
			}
			m.seenTasks[t.UPID] = true
			if first || t.Type != "vzdump" || t.Status == "OK" || strings.HasPrefix(t.Status, "WARNINGS") {
				continue
			}
			m.Emit("backup.failed", s.Name+": backup failed", fmt.Sprintf("%s/%s: backup task of %s ended with: %s", s.Name, t.Node, t.ID, t.Status))
		}
	}
}

func (m *Monitor) checkUpdates(ctx context.Context) {
	for _, s := range m.Poll.Snapshot() {
		if !s.Online {
			continue
		}
		c, _, ok := m.Poll.Client(s.Name)
		if !ok {
			continue
		}
		total := 0
		for _, n := range s.Nodes {
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			p, err := c.AptUpdates(cctx, n.Node)
			cancel()
			if err == nil {
				total += len(p)
			}
		}
		m.mu.Lock()
		prev := m.updCount[s.Name]
		m.updCount[s.Name] = total
		m.mu.Unlock()
		if total > prev {
			m.Emit("updates.available", fmt.Sprintf("%s: %d updates available", s.Name, total), fmt.Sprintf("%s has %d pending package updates.", s.Name, total))
		}
	}
}

// ---- scheduler

func (m *Monitor) runSchedules(ctx context.Context, minute time.Time) {
	list, err := m.St.Schedules()
	if err != nil {
		return
	}
	for _, sc := range list {
		if !sc.Enabled || sc.LastRun >= minute.Unix() {
			continue
		}
		spec, ok := m.cronCache[sc.Cron]
		if !ok {
			if spec, err = cron.Parse(sc.Cron); err != nil {
				continue
			}
			m.cronCache[sc.Cron] = spec
		}
		if !spec.Matches(minute) {
			continue
		}
		go m.execute(ctx, sc)
	}
}

func (m *Monitor) execute(ctx context.Context, sc store.Schedule) {
	m.St.MarkScheduleRun(sc.ID, "running") // claims this minute so a slow run is never started twice
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := m.perform(cctx, sc)
	status := "ok"
	if err != nil {
		status = err.Error()
		m.Emit("job.failed", "Schedule failed: "+sc.Name, fmt.Sprintf("%s (%s on %s): %v", sc.Name, sc.Action, sc.Server, err))
	}
	m.St.MarkScheduleRun(sc.ID, status)
	m.St.Audit("scheduler", "schedule."+sc.Action, fmt.Sprintf("%s/%s/%d", sc.Server, sc.GType, sc.VMID), sc.Name, err == nil)
}

func (m *Monitor) perform(ctx context.Context, sc store.Schedule) error {
	c, _, ok := m.Poll.Client(sc.Server)
	if !ok {
		return fmt.Errorf("unknown server %q", sc.Server)
	}
	if sc.Action == "apt-refresh" {
		for _, s := range m.Poll.Snapshot() {
			if s.Name == sc.Server {
				for _, n := range s.Nodes {
					if _, err := c.AptRefresh(ctx, n.Node); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	g, ok := m.Poll.FindGuest(sc.Server, sc.GType, sc.VMID)
	if !ok {
		return fmt.Errorf("guest %s/%d not found", sc.GType, sc.VMID)
	}
	switch sc.Action {
	case "start", "shutdown", "reboot":
		_, err := c.Power(ctx, g.Node, g.Type, g.VMID, sc.Action)
		return err
	case "backup":
		mode := sc.Params["mode"]
		if mode == "" {
			mode = "snapshot"
		}
		_, err := c.BackupNow(ctx, g.Node, g.VMID, sc.Params["storage"], mode)
		return err
	}
	return fmt.Errorf("unknown action %q", sc.Action)
}
