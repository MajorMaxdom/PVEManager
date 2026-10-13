// Package sshx runs one fixed command on a PVE host over SSH with host-key pinning.
package sshx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

type Target struct {
	Host        string // host or host:port
	User        string
	KeyFile     string
	Fingerprint string // SHA256:... of the server host key; required
}

// Run executes cmd and calls line for every output line (stdout and stderr merged).
// It returns the remote exit status as error if non-zero.
func Run(ctx context.Context, t Target, cmd string, line func(string)) error {
	if t.Fingerprint == "" {
		return errors.New("no SSH host key fingerprint configured for this server")
	}
	pem, err := os.ReadFile(t.KeyFile)
	if err != nil {
		return fmt.Errorf("ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return fmt.Errorf("ssh key: %w", err)
	}
	host := t.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "22")
	}
	cfg := &ssh.ClientConfig{
		User: t.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(k); got != t.Fingerprint {
				return fmt.Errorf("SSH host key mismatch (got %s)", got)
			}
			return nil
		},
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519}, // the pinned fingerprint is the ed25519 host key
		Timeout:           10 * time.Second,
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", host)
	if err != nil {
		return err
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, host, cfg)
	if err != nil {
		conn.Close()
		return err
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	out, _ := sess.StdoutPipe()
	sess.Stderr = pipeWriter{line}
	go func() { <-ctx.Done(); sess.Close(); client.Close() }()
	if err := sess.Start(cmd); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line(sc.Text())
	}
	return sess.Wait()
}

// pipeWriter turns stderr chunks into lines (apt writes progress to stderr).
type pipeWriter struct{ line func(string) }

func (p pipeWriter) Write(b []byte) (int, error) {
	sc := bufio.NewScanner(newReader(b))
	for sc.Scan() {
		p.line(sc.Text())
	}
	return len(b), nil
}

func newReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct{ b []byte }

func (s *sliceReader) Read(p []byte) (int, error) {
	if len(s.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.b)
	s.b = s.b[n:]
	return n, nil
}
