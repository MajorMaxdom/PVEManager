// Package poller keeps an in-memory snapshot of all servers and fans out change events.
package poller

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"pvemanager/internal/pve"
	"pvemanager/internal/store"
)

type ServerState struct {
	Name     string           `json:"name"`
	URL      string           `json:"url"`
	Online   bool             `json:"online"`
	Error    string           `json:"error,omitempty"`
	Updated  int64            `json:"updated"`
	Nodes    []pve.NodeInfo   `json:"nodes"`
	Guests   []pve.Guest      `json:"guests"`
	Storages []pve.StorageRes `json:"storages"`
	CanSSH   bool             `json:"canUpgrade"`
}

type Poller struct {
	mu      sync.RWMutex
	clients map[string]*pve.Client
	meta    map[string]store.Server
	state   map[string]*ServerState
	subs    map[chan []byte]struct{}
	Every   time.Duration
}

func New() *Poller {
	return &Poller{clients: map[string]*pve.Client{}, meta: map[string]store.Server{}, state: map[string]*ServerState{},
		subs: map[chan []byte]struct{}{}, Every: 5 * time.Second}
}

// SetServers replaces the monitored set.
func (p *Poller) SetServers(list []store.Server) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients, p.meta = map[string]*pve.Client{}, map[string]store.Server{}
	keep := map[string]bool{}
	for _, s := range list {
		p.clients[s.Name] = pve.New(s.URL, s.Token, s.Fingerprint)
		p.meta[s.Name] = s
		keep[s.Name] = true
		if p.state[s.Name] == nil {
			p.state[s.Name] = &ServerState{Name: s.Name, URL: s.URL, CanSSH: s.SSHUser != ""}
		}
	}
	for n := range p.state {
		if !keep[n] {
			delete(p.state, n)
		}
	}
}

// Client returns the API client and metadata for a server.
func (p *Poller) Client(name string) (*pve.Client, store.Server, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	c, ok := p.clients[name]
	return c, p.meta[name], ok
}

// FindGuest looks a guest up in the last snapshot.
func (p *Poller) FindGuest(server, typ string, vmid int) (pve.Guest, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if st := p.state[server]; st != nil {
		for _, g := range st.Guests {
			if g.VMID == vmid && g.Type == typ {
				return g, true
			}
		}
	}
	return pve.Guest{}, false
}

func (p *Poller) Snapshot() []ServerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ServerState, 0, len(p.state))
	for _, s := range p.state {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (p *Poller) Subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 4)
	p.mu.Lock()
	p.subs[ch] = struct{}{}
	p.mu.Unlock()
	return ch, func() { p.mu.Lock(); delete(p.subs, ch); p.mu.Unlock() }
}

func (p *Poller) broadcast() {
	b, _ := json.Marshal(p.Snapshot())
	p.mu.RLock()
	defer p.mu.RUnlock()
	for ch := range p.subs {
		select {
		case ch <- b:
		default: // slow consumer: drop, next tick brings fresh state
		}
	}
}

// RefreshNow polls one server immediately (e.g. after a power action).
func (p *Poller) RefreshNow(ctx context.Context, name string) {
	p.refresh(ctx, name)
	p.broadcast()
}

func (p *Poller) refresh(ctx context.Context, name string) {
	c, _, ok := p.Client(name)
	if !ok {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	guests, nodes, stores, err := c.ResourcesFull(cctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state[name]
	if st == nil {
		return
	}
	st.Updated = time.Now().Unix()
	if err != nil {
		st.Online, st.Error = false, err.Error()
		return
	}
	st.Online, st.Error, st.Guests, st.Nodes, st.Storages = true, "", guests, nodes, stores
}

// Run polls all servers until ctx ends.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(p.Every)
	defer t.Stop()
	for {
		p.mu.RLock()
		names := make([]string, 0, len(p.clients))
		for n := range p.clients {
			names = append(names, n)
		}
		p.mu.RUnlock()
		var wg sync.WaitGroup
		for _, n := range names {
			wg.Add(1)
			go func(n string) { defer wg.Done(); p.refresh(ctx, n) }(n)
		}
		wg.Wait()
		p.broadcast()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
