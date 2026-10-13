package pve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	reFile     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	reTemplate = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	reHexSum   = regexp.MustCompile(`^[0-9a-fA-F]{32,128}$`)
)

// CheckMediaFile validates the target file name for the given content type.
func CheckMediaFile(content, name string) error {
	if !reFile.MatchString(name) {
		return errors.New("invalid file name (letters, digits, . _ + - only)")
	}
	l := strings.ToLower(name)
	switch content {
	case "iso":
		if !strings.HasSuffix(l, ".iso") && !strings.HasSuffix(l, ".img") {
			return errors.New("ISO files must end in .iso or .img")
		}
	case "vztmpl":
		if !strings.HasSuffix(l, ".tar.gz") && !strings.HasSuffix(l, ".tar.xz") && !strings.HasSuffix(l, ".tar.zst") {
			return errors.New("templates must end in .tar.gz, .tar.xz or .tar.zst")
		}
	default:
		return errors.New("content must be iso or vztmpl")
	}
	return nil
}

type MediaStorage struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	Avail   int64  `json:"avail"`
	Content string `json:"content"`
}

type MediaItem struct {
	VolID   string `json:"volid"`
	Size    int64  `json:"size"`
	CTime   int64  `json:"ctime"`
	Format  string `json:"format"`
	Content string `json:"content"`
	Storage string `json:"storage"`
}

type Media struct {
	Storages []MediaStorage `json:"storages"`
	Items    []MediaItem    `json:"items"`
}

func (c *Client) Media(ctx context.Context, node string) (Media, error) {
	m := Media{Storages: []MediaStorage{}, Items: []MediaItem{}}
	if !validNode.MatchString(node) {
		return m, errors.New("invalid node")
	}
	var st []struct {
		Storage string `json:"storage"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Avail   int64  `json:"avail"`
		Active  int    `json:"active"`
	}
	if err := c.get(ctx, "/nodes/"+node+"/storage", &st); err != nil {
		return m, err
	}
	for _, s := range st {
		if s.Active == 0 || !reStorage.MatchString(s.Storage) {
			continue
		}
		kinds := []string{}
		for _, k := range []string{"iso", "vztmpl"} {
			for _, p := range strings.Split(s.Content, ",") {
				if p == k {
					kinds = append(kinds, k)
				}
			}
		}
		if len(kinds) == 0 {
			continue
		}
		m.Storages = append(m.Storages, MediaStorage{s.Storage, s.Type, s.Avail, strings.Join(kinds, ",")})
		for _, k := range kinds {
			var items []MediaItem
			if err := c.get(ctx, fmt.Sprintf("/nodes/%s/storage/%s/content?content=%s", node, s.Storage, k), &items); err == nil {
				for _, it := range items {
					it.Content, it.Storage = k, s.Storage
					m.Items = append(m.Items, it)
				}
			}
		}
	}
	sort.Slice(m.Items, func(i, j int) bool { return m.Items[i].VolID < m.Items[j].VolID })
	return m, nil
}

// DownloadURL lets the PVE host fetch a file itself (no data passes through pve-manager).
func (c *Client) DownloadURL(ctx context.Context, node, storage, content, rawURL, filename, checksum, algo string) (string, error) {
	if !validNode.MatchString(node) || !reStorage.MatchString(storage) {
		return "", errors.New("invalid node or storage")
	}
	if err := CheckMediaFile(content, filename); err != nil {
		return "", err
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", errors.New("url must be http(s)://host/path without credentials")
	}
	f := url.Values{"url": {rawURL}, "content": {content}, "filename": {filename}}
	if checksum != "" {
		if !reHexSum.MatchString(checksum) || (algo != "sha256" && algo != "sha512" && algo != "sha1" && algo != "md5") {
			return "", errors.New("invalid checksum or algorithm")
		}
		f.Set("checksum", checksum)
		f.Set("checksum-algorithm", algo)
	}
	var upid string
	return upid, c.post(ctx, fmt.Sprintf("/nodes/%s/storage/%s/download-url", node, storage), f, &upid)
}

// Upload streams r (exactly size bytes) to the storage as a multipart upload.
func (c *Client) Upload(ctx context.Context, node, storage, content, filename string, size int64, r io.Reader) (string, error) {
	if !validNode.MatchString(node) || !reStorage.MatchString(storage) {
		return "", errors.New("invalid node or storage")
	}
	if err := CheckMediaFile(content, filename); err != nil {
		return "", err
	}
	if size <= 0 {
		return "", errors.New("upload needs a Content-Length")
	}
	const boundary = "pvmgrXXXXboundary7f3a9c1e"
	head := fmt.Sprintf("--%s\r\nContent-Disposition: form-data; name=\"content\"\r\n\r\n%s\r\n--%s\r\nContent-Disposition: form-data; name=\"filename\"; filename=\"%s\"\r\nContent-Type: application/octet-stream\r\n\r\n",
		boundary, content, boundary, filename)
	tail := fmt.Sprintf("\r\n--%s--\r\n", boundary)
	body := io.MultiReader(strings.NewReader(head), io.LimitReader(r, size), strings.NewReader(tail))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api2/json/nodes/%s/storage/%s/upload", c.BaseURL, node, storage), body)
	if err != nil {
		return "", err
	}
	req.ContentLength = int64(len(head)) + size + int64(len(tail)) // pveproxy needs a length, chunked is refused
	req.Header.Set("Authorization", "PVEAPIToken="+c.Token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	long := &http.Client{Transport: c.HTTP.Transport} // no overall timeout: ISOs are large
	resp, err := long.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
	}
	// {"data":"UPID:..."}
	s := string(raw)
	if i := strings.Index(s, "UPID:"); i >= 0 {
		if j := strings.IndexByte(s[i:], '"'); j > 0 {
			return s[i : i+j], nil
		}
	}
	return "", nil
}

type Appliance struct {
	Template    string `json:"template"`
	Package     string `json:"package"`
	Description string `json:"description"`
	OS          string `json:"os"`
	Version     string `json:"version"`
	Headline    string `json:"headline"`
	Section     string `json:"section"`
}

func (c *Client) Appliances(ctx context.Context, node string) ([]Appliance, error) {
	if !validNode.MatchString(node) {
		return nil, errors.New("invalid node")
	}
	var l []Appliance
	if err := c.get(ctx, "/nodes/"+node+"/aplinfo", &l); err != nil {
		return nil, err
	}
	out := l[:0]
	for _, a := range l {
		if a.Section == "system" && reTemplate.MatchString(a.Template) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Package < out[j].Package || (out[i].Package == out[j].Package && out[i].Version > out[j].Version)
	})
	return out, nil
}

// DownloadAppliance fetches a template from the official catalogue to the storage.
func (c *Client) DownloadAppliance(ctx context.Context, node, storage, template string) (string, error) {
	if !validNode.MatchString(node) || !reStorage.MatchString(storage) || !reTemplate.MatchString(template) {
		return "", errors.New("invalid request")
	}
	var upid string
	return upid, c.post(ctx, "/nodes/"+node+"/aplinfo", url.Values{"storage": {storage}, "template": {template}}, &upid)
}
