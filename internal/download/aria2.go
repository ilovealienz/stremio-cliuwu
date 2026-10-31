package download

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// aria2Conns is the connections per file. TorBox asks for 4 or less.
const aria2Conns = 4

var errNoAria2 = errors.New("aria2c not found — install it or turn it off under downloads in settings")

func aria2Path() (string, error) {
	p, err := exec.LookPath("aria2c")
	if err != nil {
		return "", errNoAria2
	}
	return p, nil
}

// Aria2Found reports whether aria2c is installed.
func Aria2Found() bool {
	_, err := aria2Path()
	return err == nil
}

// probe follows the link's redirects and reads its size and file name, and
// whether the server takes ranges, which resuming needs. aria2c asks the
// original link again for every connection, which for an addon's resolve link
// means asking the addon each time.
func probe(ctx context.Context, link string) (final, name string, size int64, ranges bool, err error) {
	client := *downloadClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for hops := 0; hops < 10; hops++ {
		req, err := http.NewRequestWithContext(ctx, "GET", link, nil)
		if err != nil {
			return "", "", 0, false, err
		}
		req.Header.Set("User-Agent", downloadUA)
		req.Header.Set("Range", "bytes=0-0")
		res, err := client.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				return "", "", 0, false, ue.Err // the URL can carry a debrid key
			}
			return "", "", 0, false, err
		}
		res.Body.Close()

		switch {
		case res.StatusCode >= 300 && res.StatusCode < 400:
			loc, err := res.Location()
			if err != nil {
				return "", "", 0, false, fmt.Errorf("server said %s", res.Status)
			}
			link = loc.String()
			continue
		case res.StatusCode == http.StatusForbidden, res.StatusCode == http.StatusUnauthorized:
			return "", "", 0, false, fmt.Errorf("%s — the link may have expired, press R on the stream list to refetch", res.Status)
		case res.StatusCode >= 400:
			return "", "", 0, false, fmt.Errorf("server said %s", res.Status)
		}

		size = res.ContentLength
		if cr := res.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndexByte(cr, '/'); i >= 0 {
				size, _ = strconv.ParseInt(cr[i+1:], 10, 64)
			}
		}
		if size < 0 {
			size = 0
		}
		name, _ = url.PathUnescape(path.Base(res.Request.URL.Path))
		return link, name, size, res.StatusCode == http.StatusPartialContent, nil
	}
	return "", "", 0, false, errors.New("too many redirects")
}

// aria2Proc is one running aria2c, its RPC endpoint and its one download.
type aria2Proc struct {
	cmd    *exec.Cmd
	port   int
	secret string
	gid    string
	exited chan struct{}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startAria2 runs aria2c and hands it the link over RPC rather than on the
// command line, where other users could read it.
func startAria2(bin, dir, out, link string) (*aria2Proc, error) {
	var lastErr error
	for try := 0; try < 3; try++ { // the port can be taken before aria2c binds it
		port, err := freePort()
		if err != nil {
			return nil, err
		}
		b := make([]byte, 16)
		rand.Read(b)
		p := &aria2Proc{port: port, secret: hex.EncodeToString(b), exited: make(chan struct{})}
		p.cmd = exec.Command(bin,
			"--no-conf", "--enable-rpc", "--rpc-listen-port="+strconv.Itoa(port), "--rpc-secret="+p.secret,
			"--stop-with-process="+strconv.Itoa(os.Getpid()), "--quiet=true",
			"--file-allocation=none", "--continue=true", "--auto-file-renaming=false", "--allow-overwrite=false",
			"--max-connection-per-server="+strconv.Itoa(aria2Conns), "--split="+strconv.Itoa(aria2Conns), "--min-split-size=8M",
			"--max-tries=5", "--retry-wait=2", "--connect-timeout=15", "--timeout=60", "--auto-save-interval=10",
			"--user-agent="+downloadUA)
		if err := p.cmd.Start(); err != nil {
			return nil, fmt.Errorf("couldn't start aria2c: %w", err)
		}
		go func() { p.cmd.Wait(); close(p.exited) }()

		// Up once the RPC answers; gone at once if the port was taken.
		for i := 0; i < 50; i++ {
			if _, err := p.call("aria2.getVersion"); err == nil {
				raw, err := p.call("aria2.addUri", []string{link}, map[string]string{"dir": dir, "out": out})
				if err != nil || json.Unmarshal(raw, &p.gid) != nil {
					p.kill()
					return nil, fmt.Errorf("aria2c refused the download: %v", err)
				}
				return p, nil
			}
			select {
			case <-p.exited:
				i = 50
			case <-time.After(100 * time.Millisecond):
			}
		}
		p.kill()
		lastErr = errors.New("aria2c didn't start")
	}
	return nil, lastErr
}

func (p *aria2Proc) call(method string, params ...any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "1", "method": method,
		"params": append([]any{"token:" + p.secret}, params...),
	})
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Post("http://127.0.0.1:"+strconv.Itoa(p.port)+"/jsonrpc", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Error != nil {
		return nil, errors.New(r.Error.Message)
	}
	return r.Result, nil
}

type aria2Status struct {
	Status    string `json:"status"`
	Completed int64  `json:"completedLength,string"`
	Total     int64  `json:"totalLength,string"`
	Speed     int64  `json:"downloadSpeed,string"`
	ErrorCode string `json:"errorCode"`
	ErrorMsg  string `json:"errorMessage"`
}

var statusKeys = []string{"status", "completedLength", "totalLength", "downloadSpeed", "errorCode", "errorMessage"}

func (p *aria2Proc) status() (aria2Status, error) {
	var st aria2Status
	raw, err := p.call("aria2.tellStatus", p.gid, statusKeys)
	if err == nil {
		err = json.Unmarshal(raw, &st)
	}
	return st, err
}

// stop ends aria2c, which saves its resume file on the way out.
func (p *aria2Proc) stop() {
	select {
	case <-p.exited:
		return
	default:
	}
	if !terminate(p.cmd.Process) {
		p.call("aria2.forceShutdown")
	}
	select {
	case <-p.exited:
	case <-time.After(10 * time.Second):
		p.kill()
	}
}

func (p *aria2Proc) kill() {
	p.cmd.Process.Kill()
	<-p.exited
}

// aria2Error turns aria2c's error into what to do about it.
func aria2Error(st aria2Status) error {
	msg := strings.TrimSpace(st.ErrorMsg)
	switch {
	case strings.Contains(msg, "status=404"), st.ErrorCode == "3":
		return errors.New("the file isn't on the server any more")
	case st.ErrorCode == "22", st.ErrorCode == "24":
		// The server refused the link. With several connections open aria2c
		// often says only "No URI available".
		return errors.New("the link may have expired — press R on the stream list to refetch")
	case st.ErrorCode == "9":
		return errors.New("not enough disk space")
	}
	if msg == "" {
		msg = "error " + st.ErrorCode
	}
	return errors.New("aria2c: " + msg)
}

func (d *Downloader) fetchAria2(dl *Download, bin string) error {
	ctx, abort := context.WithCancel(context.Background())
	defer abort()
	d.mu.Lock()
	d.abort[dl.ID] = abort
	stopped := d.cancel[dl.ID]
	d.mu.Unlock()
	if stopped {
		return errStopped
	}

	final, name, total, ranges, err := probe(ctx, dl.URL)
	if err != nil {
		if ctx.Err() != nil {
			return errStopped
		}
		return err
	}

	// Carry on only from the same file at the same size, on a server that
	// takes ranges: this link, or a refetched one naming the same file.
	// aria2c can't tell two files of one size apart.
	pi, ok := readPartInfo(dl.Path)
	same := ok && ranges && pi.Total == total && (pi.URL == dl.URL || pi.Name != "" && pi.Name == name && total > 0)
	if !same {
		deleteFiles(dl.Path)
		pi = partInfo{}
	}
	done := partDone(dl.Path, pi)

	if need := total - done; total > 0 && need > 0 {
		dir := filepath.Dir(dl.Path)
		if avail, ok := freeSpace(dir); ok && avail < uint64(need) {
			return fmt.Errorf("not enough space in %s — %s free, needs %s",
				dir, FmtBytes(int64(avail)), FmtBytes(need))
		}
	}

	d.mu.Lock()
	dl.Done, dl.Total, dl.Resume = done, total, done > 0
	d.mu.Unlock()
	writePartInfo(dl.Path, partInfo{URL: dl.URL, Total: total, Label: dl.Label, Name: name, Done: done})

	p, err := startAria2(bin, filepath.Dir(dl.Path), filepath.Base(partPath(dl.Path)), final)
	if err != nil {
		return err
	}
	defer p.stop()

	// The stop key aborts the context, which the loop below waits on.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	ticks := 0
	save := func(n int64) {
		writePartInfo(dl.Path, partInfo{URL: dl.URL, Total: total, Label: dl.Label, Name: name, Done: n})
	}
	for {
		select {
		case <-ctx.Done():
			d.mu.Lock()
			n := dl.Done
			d.mu.Unlock()
			if st, err := p.status(); err == nil {
				n = st.Completed // exact, rather than the last tick's
			}
			p.stop()
			save(n)
			return errStopped
		case <-p.exited:
			return errors.New("aria2c stopped unexpectedly")
		case <-tick.C:
		}

		st, err := p.status()
		if err != nil {
			continue // a slow answer; the next tick asks again
		}
		d.mu.Lock()
		if st.Total > 0 {
			dl.Total = st.Total
		}
		dl.Done, dl.Speed = st.Completed, st.Speed
		d.mu.Unlock()
		d.emit(TickMsg{})
		if ticks++; ticks%10 == 0 && st.Status == "active" {
			save(st.Completed) // in step with aria2c's own resume file
		}

		switch st.Status {
		case "complete":
			p.stop()
			if got := fileSize(partPath(dl.Path)); total > 0 && got != total {
				deleteFiles(dl.Path) // no resume file is left to carry on from
				return fmt.Errorf("aria2c finished at %s of %s — enter to start again", FmtBytes(got), FmtBytes(total))
			}
			if err := os.Rename(partPath(dl.Path), dl.Path); err != nil {
				return err
			}
			os.Remove(infoPath(dl.Path))
			os.Remove(controlPath(dl.Path))
			return nil
		case "error", "removed":
			p.stop()
			save(st.Completed)
			if st.ErrorCode == "1" && strings.Contains(st.ErrorMsg, "aborted") {
				deleteFiles(dl.Path) // the partial doesn't fit this file; start over next time
			}
			return aria2Error(st)
		}
	}
}
