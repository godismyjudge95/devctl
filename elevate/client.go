package elevate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

// DaemonClient talks to `devctl elevate daemon` over the unix socket.
type DaemonClient struct {
	http *http.Client
}

// NewDaemonClient returns a client for the elevate control socket.
func NewDaemonClient(serverRoot string) *DaemonClient {
	sock := paths.ElevateSocket(serverRoot)
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}
	return &DaemonClient{http: &http.Client{Transport: tr, Timeout: 8 * time.Second}}
}

func (c *DaemonClient) Start(def services.Definition) error {
	return c.post("/services/" + def.ID + "/start")
}

func (c *DaemonClient) Stop(id string) error {
	return c.post("/services/" + id + "/stop")
}

func (c *DaemonClient) Restart(def services.Definition) error {
	return c.post("/services/" + def.ID + "/restart")
}

func (c *DaemonClient) IsRunning(id string) bool {
	resp, err := c.http.Get("http://elevate/services/" + id)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Running bool `json:"running"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return false
	}
	return body.Running
}

func (c *DaemonClient) post(path string) error {
	resp, err := c.http.Post("http://elevate"+path, "application/json", nil)
	if err != nil {
		return fmt.Errorf("elevate daemon: %w (is `devctl elevate daemon` running?)", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		msg := string(b)
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("elevate daemon: %s", msg)
	}
	return nil
}
