package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type consoleSession struct {
	server, typ, vmid, node, path string
	user                          string
	expires                       time.Time
}

// consoleOpen asks PVE for a VNC session and returns a one-time id the browser then upgrades on.
func (s *Server) consoleOpen(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	server, typ, vmid := r.PathValue("server"), r.PathValue("type"), r.PathValue("vmid")
	c, g, err := s.resolveGuest(server, typ, vmid)
	if err != nil {
		jerr(w, 404, err.Error())
		return
	}
	px, err := c.OpenConsole(r.Context(), g.Node, typ, g.VMID)
	s.St.Audit(u.Name, "console.open", fmt.Sprintf("%s/%s/%s", server, typ, vmid), g.Name, err == nil)
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	id := randID()
	s.consoles.Store(id, &consoleSession{server: server, typ: typ, vmid: vmid, node: g.Node, path: px.WSPath, user: u.Name, expires: time.Now().Add(30 * time.Second)})
	jsonOut(w, 200, map[string]string{"id": id, "password": px.Ticket})
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"binary"},
	CheckOrigin: func(r *http.Request) bool { // same-origin only
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		ou, err := url.Parse(o)
		return err == nil && strings.EqualFold(ou.Host, r.Host)
	},
}

func (s *Server) consoleWS(w http.ResponseWriter, r *http.Request) {
	v, ok := s.consoles.LoadAndDelete(r.PathValue("id")) // one-time use
	if !ok {
		jerr(w, 404, "console session unknown or used")
		return
	}
	cs := v.(*consoleSession)
	if time.Now().After(cs.expires) || cs.user != userFrom(r).Name {
		jerr(w, 410, "console session expired")
		return
	}
	c, _, ok := s.Poll.Client(cs.server)
	if !ok {
		jerr(w, 404, "unknown server")
		return
	}
	base, _ := url.Parse(c.BaseURL)
	d := websocket.Dialer{TLSClientConfig: c.TLSConfig(), Subprotocols: []string{"binary"}, HandshakeTimeout: 10 * time.Second}
	up, resp, err := d.DialContext(r.Context(), "wss://"+base.Host+cs.path, http.Header{"Authorization": {"PVEAPIToken=" + c.Token}})
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		s.Log.Warn("console dial failed", "server", cs.server, "err", err, "status", code)
		jerr(w, 502, "could not reach console on PVE host")
		return
	}
	defer up.Close()
	down, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer down.Close()
	pipe(down, up)
}

// pipe copies frames both ways until either side closes.
func pipe(a, b *websocket.Conn) {
	var once sync.Once
	done := make(chan struct{})
	stop := func() { once.Do(func() { close(done) }) }
	cp := func(dst, src *websocket.Conn) {
		defer stop()
		for {
			mt, msg, err := src.ReadMessage()
			if err != nil {
				return
			}
			if err := dst.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
}
