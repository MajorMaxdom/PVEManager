package pve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken=u@pve!t=s" {
			http.Error(w, `{"data":null,"message":"no ticket"}`, 401)
			return
		}
		switch {
		case r.URL.Path == "/api2/json/version":
			w.Write([]byte(`{"data":{"version":"9.2.21"}}`))
		case r.URL.Path == "/api2/json/cluster/resources":
			w.Write([]byte(`{"data":[
			 {"type":"node","node":"n1","status":"online","cpu":0.1,"maxcpu":4,"mem":1,"maxmem":2,"disk":3,"maxdisk":4,"uptime":9},
			 {"type":"qemu","node":"n1","vmid":101,"name":"web","status":"running","cpu":0.5,"maxcpu":2,"mem":10,"maxmem":20},
			 {"type":"lxc","node":"n1","vmid":100,"name":"ct","status":"stopped"},
			 {"type":"storage","node":"n1","storage":"local"}]}`))
		case strings.HasSuffix(r.URL.Path, "/status/start") && r.Method == "POST":
			w.Write([]byte(`{"data":"UPID:n1:1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().Raw)
	return srv, hex.EncodeToString(sum[:])
}

func TestResourcesAndPower(t *testing.T) {
	srv, fp := fixture(t)
	c := New(srv.URL, "u@pve!t=s", fp)
	ctx := context.Background()
	if v, err := c.Version(ctx); err != nil || v != "9.2.21" {
		t.Fatalf("version %q %v", v, err)
	}
	g, n, err := c.Resources(ctx)
	if err != nil || len(g) != 2 || len(n) != 1 {
		t.Fatalf("resources %v %v %v", g, n, err)
	}
	if g[0].VMID != 100 || g[0].Type != "lxc" || g[1].CPUs != 2 || g[1].Mem != 10 || n[0].MaxCPU != 4 {
		t.Fatalf("fields lost in parsing: %+v %+v", g, n)
	}
	if up, err := c.Power(ctx, "n1", "qemu", 101, "start"); err != nil || up != "UPID:n1:1" {
		t.Fatalf("power %q %v", up, err)
	}
}

func TestPinningAndInputValidation(t *testing.T) {
	srv, fp := fixture(t)
	bad := New(srv.URL, "u@pve!t=s", strings.Repeat("00", 32))
	if _, err := bad.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("wrong pin accepted: %v", err)
	}
	c := New(srv.URL, "wrong", fp)
	if _, err := c.Version(context.Background()); err == nil {
		t.Fatal("bad token accepted")
	} else if ae, ok := err.(*APIError); !ok || ae.Status != 401 {
		t.Fatalf("want 401 APIError, got %v", err)
	}
	for _, args := range [][3]string{{"n/../x", "qemu", "start"}, {"n1", "evil", "start"}, {"n1", "qemu", "format"}} {
		if _, err := c.Power(context.Background(), args[0], args[1], 1, args[2]); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestSpecValidationAndForm(t *testing.T) {
	ok := VMSpec{VMID: 200, Name: "web-1", Cores: 2, MemMiB: 2048, DiskGiB: 16, Storage: "local-lvm", Bridge: "vmbr0", ISO: "local:iso/debian.iso", UEFI: true, Agent: true}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	f := ok.Form()
	if f.Get("scsi0") != "local-lvm:16" || f.Get("net0") != "virtio,bridge=vmbr0" || f.Get("bios") != "ovmf" || f.Get("boot") != "order=ide2;scsi0" {
		t.Fatalf("form wrong: %v", f)
	}
	bad := []VMSpec{
		func() VMSpec { v := ok; v.Name = "a b"; return v }(),
		func() VMSpec { v := ok; v.Storage = "x,y=z"; return v }(),
		func() VMSpec { v := ok; v.Bridge = "vmbr0,firewall=0"; return v }(),
		func() VMSpec { v := ok; v.ISO = "local:iso/../../etc/passwd"; return v }(),
		func() VMSpec { v := ok; v.VLAN = "5000"; return v }(),
		func() VMSpec { v := ok; v.VMID = 5; return v }(),
	}
	for i, v := range bad {
		if v.Validate() == nil {
			t.Errorf("bad spec %d accepted", i)
		}
	}
	ct := CTSpec{VMID: 201, Hostname: "ct1", Template: "local:vztmpl/debian-13.tar.zst", Cores: 1, MemMiB: 512, DiskGiB: 8, Storage: "local-lvm", Bridge: "vmbr0", IP: "10.0.0.5/24", Gateway: "10.0.0.1", Password: "longenough1", Unprivileged: true}
	if err := ct.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := ct.Form().Get("net0"); got != "name=eth0,bridge=vmbr0,ip=10.0.0.5/24,gw=10.0.0.1" {
		t.Fatalf("net0 %q", got)
	}
	ct.Password, ct.SSHKey = "", ""
	if ct.Validate() == nil {
		t.Error("CT without credentials accepted")
	}
	ct.SSHKey = "ssh-ed25519 AAAA\nssh-rsa evil"
	if ct.Validate() == nil {
		t.Error("multi-line ssh key accepted")
	}
}

func TestOpsValidation(t *testing.T) {
	srv, fp := fixture(t)
	c := New(srv.URL, "u@pve!t=s", fp)
	ctx := context.Background()
	for _, bad := range []string{"1bad", "a b", "a;b", "", strings.Repeat("a", 41), "../x"} {
		if _, err := c.CreateSnapshot(ctx, "n1", "qemu", 100, bad, "", false); err == nil {
			t.Errorf("snapshot name %q accepted", bad)
		}
		if _, err := c.RollbackSnapshot(ctx, "n1", "qemu", 100, bad); err == nil {
			t.Errorf("rollback name %q accepted", bad)
		}
	}
	if _, err := c.CreateSnapshot(ctx, "n1", "qemu", 100, "ok", "two\nlines", false); err == nil {
		t.Error("multi-line description accepted")
	}
	if err := c.GrowDisk(ctx, "n1", "qemu", 100, "scsi0,discard=on", 5); err == nil {
		t.Error("disk key with options accepted")
	}
	if err := c.GrowDisk(ctx, "n1", "qemu", 100, "scsi0", 0); err == nil {
		t.Error("zero growth accepted")
	}
	for _, v := range []string{"local:backup/../../etc/passwd", "local:iso/x.iso", "x"} {
		if _, err := c.Restore(ctx, "n1", "qemu", 100, v, true); err == nil {
			t.Errorf("restore volid %q accepted", v)
		}
	}
	if _, err := c.BackupNow(ctx, "n1", 100, "local", "evil"); err == nil {
		t.Error("bad backup mode accepted")
	}
	if _, err := c.RRD(ctx, "n1", "qemu", 100, "decade"); err == nil {
		t.Error("bad timeframe accepted")
	}
}

func TestMediaValidationAndUploadFraming(t *testing.T) {
	for _, tc := range []struct {
		content, name string
		ok            bool
	}{
		{"iso", "debian-13.iso", true}, {"iso", "x.img", true}, {"iso", "evil.sh", false}, {"iso", "../a.iso", false},
		{"iso", "a b.iso", false}, {"vztmpl", "debian.tar.zst", true}, {"vztmpl", "debian.iso", false}, {"other", "a.iso", false},
		{"iso", "a\r\nX: y.iso", false},
	} {
		if err := CheckMediaFile(tc.content, tc.name); (err == nil) != tc.ok {
			t.Errorf("CheckMediaFile(%q,%q) = %v, want ok=%v", tc.content, tc.name, err, tc.ok)
		}
	}
	var gotLen int64
	var gotBody string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLen = r.ContentLength
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte(`{"data":"UPID:n1:abc:1:2:imgcopy::u@pve!t:"}`))
	}))
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	c := New(srv.URL, "u@pve!t=s", hex.EncodeToString(sum[:]))
	payload := strings.Repeat("A", 1000) + "TRAILING-DATA-MUST-NOT-LEAK"
	upid, err := c.Upload(context.Background(), "n1", "local", "iso", "x.iso", 1000, strings.NewReader(payload))
	if err != nil || !strings.HasPrefix(upid, "UPID:n1:abc") {
		t.Fatalf("upload: %q %v", upid, err)
	}
	if int64(len(gotBody)) != gotLen {
		t.Fatalf("Content-Length %d does not match body %d", gotLen, len(gotBody))
	}
	if strings.Contains(gotBody, "TRAILING") || !strings.Contains(gotBody, `filename="x.iso"`) || !strings.HasSuffix(gotBody, "--\r\n") {
		t.Fatalf("bad multipart body (size limit or framing)")
	}
	if _, err := c.DownloadURL(context.Background(), "n1", "local", "iso", "ftp://x/a.iso", "a.iso", "", ""); err == nil {
		t.Error("ftp url accepted")
	}
	if _, err := c.DownloadURL(context.Background(), "n1", "local", "iso", "https://u:p@x/a.iso", "a.iso", "", ""); err == nil {
		t.Error("url with credentials accepted")
	}
	if _, err := c.DownloadURL(context.Background(), "n1", "local", "iso", "https://x/a.iso", "a.iso", "zz", "sha256"); err == nil {
		t.Error("bad checksum accepted")
	}
}

func TestCloudInitValidationAndEncoding(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyDataOnlyForTestsNotARealKey000 test@host"
	ok := CloudInit{User: "debian", Password: "secret-pw", SSHKeys: key + "\n\n" + key, IP: "10.0.0.5/24", Gateway: "10.0.0.1", Nameserver: "1.1.1.1 9.9.9.9", SearchDom: "lab.example"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	f := ok.Form()
	if f.Get("ipconfig0") != "ip=10.0.0.5/24,gw=10.0.0.1" || f.Get("nameserver") != "1.1.1.1 9.9.9.9" || f.Get("ciuser") != "debian" {
		t.Fatalf("form %v", f)
	}
	if sk := f.Get("sshkeys"); strings.Contains(sk, " ") || strings.Contains(sk, "+") || !strings.Contains(sk, "%20") || !strings.Contains(sk, "%0A") {
		t.Fatalf("sshkeys must be percent-encoded for PVE, got %q", sk)
	}
	bad := []CloudInit{
		{User: "Root"}, {User: "a b"}, {Password: "abc"}, {Password: "line\nbreak1"},
		{SSHKeys: "ssh-rsa AAAA\nrm -rf /"}, {SSHKeys: "not a key"},
		{IP: "10.0.0.5"}, {IP: "10.0.0.999/24"}, {IP: "dhcp", Gateway: "10.0.0.1"}, {Gateway: "10.0.0.1"},
		{IP: "10.0.0.5/24", Gateway: "x"}, {Nameserver: "1.1.1.1; reboot"}, {SearchDom: "a b"}, {DriveStore: "x,y"},
	}
	for i, c := range bad {
		if c.Validate() == nil {
			t.Errorf("bad cloud-init %d accepted: %+v", i, c)
		}
	}
	if (CloudInit{IP: "dhcp"}).Form().Get("ipconfig0") != "ip=dhcp" {
		t.Error("dhcp form")
	}
}

func TestBackupOriginAndRestoreForm(t *testing.T) {
	for _, tc := range []struct {
		volid string
		typ   string
		id    int
		ok    bool
	}{
		{"store1:backup/vzdump-qemu-100-2026_10_13-03_00_05.vma.zst", "qemu", 100, true},
		{"local:backup/vzdump-lxc-205-2026_01_01-00_00_00.tar.zst", "lxc", 205, true},
		{"local:backup/random.tar", "", 0, false},
	} {
		typ, id, ok := BackupOrigin(tc.volid)
		if typ != tc.typ || id != tc.id || ok != tc.ok {
			t.Errorf("%s -> %q %d %v", tc.volid, typ, id, ok)
		}
	}
	var got url.Values
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.Form
		w.Write([]byte(`{"data":"UPID:n1:1"}`))
	}))
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	c := New(srv.URL, "u@pve!t=s", hex.EncodeToString(sum[:]))
	vol := "local:backup/vzdump-qemu-100-2026_10_13-03_00_05.vma.zst"
	if _, err := c.Restore(context.Background(), "n1", "qemu", 150, vol, false); err != nil {
		t.Fatal(err)
	}
	if got.Get("unique") != "1" || got.Get("force") != "0" || got.Get("archive") != vol {
		t.Fatalf("new-id restore must get unique MACs and no force: %v", got)
	}
	if _, err := c.Restore(context.Background(), "n1", "qemu", 100, vol, true); err != nil {
		t.Fatal(err)
	}
	if got.Get("unique") != "" || got.Get("force") != "1" {
		t.Fatalf("overwrite keeps the original MACs: %v", got)
	}
}
