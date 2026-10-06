// Package dockerx is a small Docker Engine API client over the local socket,
// covering what Sparkbase needs to run one container per service: images,
// volumes, containers, logs, stats and exec. No SDK dependency.
package dockerx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiVersion = "v1.43"

var ErrNotFound = errors.New("docker: not found")

type Client struct {
	http *http.Client
	// stream has no timeout, for logs and long requests.
	stream *http.Client
}

// SocketPath finds the Docker socket: DOCKER_HOST, the standard path, or
// Docker Desktop's per-user socket on macOS.
func SocketPath() string {
	if h := os.Getenv("DOCKER_HOST"); strings.HasPrefix(h, "unix://") {
		return strings.TrimPrefix(h, "unix://")
	}
	candidates := []string{"/var/run/docker.sock"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".docker/run/docker.sock"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "/var/run/docker.sock"
}

func New(socket string) *Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}
	return &Client{
		http:   &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{DialContext: dial}},
		stream: &http.Client{Transport: &http.Transport{DialContext: dial}},
	}
}

func (c *Client) do(ctx context.Context, client *http.Client, method, path string, q url.Values, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := "http://docker/" + apiVersion + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 404 {
		res.Body.Close()
		return nil, ErrNotFound
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Message == "" {
			e.Message = strings.TrimSpace(string(data))
		}
		return nil, fmt.Errorf("docker %s %s: %d %s", method, path, res.StatusCode, e.Message)
	}
	return res, nil
}

func (c *Client) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	res, err := c.do(ctx, c.http, method, path, q, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.call(ctx, "GET", "/_ping", nil, nil, nil)
}

// EnsureImage pulls an image when it is not present.
func (c *Client) EnsureImage(ctx context.Context, image string) error {
	if err := c.call(ctx, "GET", "/images/"+image+"/json", nil, nil, nil); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	name, tag, _ := strings.Cut(image, ":")
	if tag == "" {
		tag = "latest"
	}
	res, err := c.do(ctx, c.http, "POST", "/images/create", url.Values{"fromImage": {name}, "tag": {tag}}, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// The pull reports progress as JSON lines; errors arrive in-band.
	dec := json.NewDecoder(res.Body)
	for {
		var msg struct {
			Error string `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if msg.Error != "" {
			return fmt.Errorf("pull %s: %s", image, msg.Error)
		}
	}
}

func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	if err := c.call(ctx, "GET", "/networks/"+name, nil, nil, nil); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return c.call(ctx, "POST", "/networks/create", nil, map[string]any{"Name": name, "Driver": "bridge", "Labels": map[string]string{"sparkbase": "1"}}, nil)
}

func (c *Client) EnsureVolume(ctx context.Context, name string, labels map[string]string) error {
	return c.call(ctx, "POST", "/volumes/create", nil, map[string]any{"Name": name, "Labels": labels}, nil)
}

func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	err := c.call(ctx, "DELETE", "/volumes/"+name, url.Values{"force": {"1"}}, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// NetworkMode is the network the container joins.
func (s Spec) NetworkMode() string { return s.Network }

// Spec describes a service container.
type Spec struct {
	Name        string
	Image       string
	Env         []string
	Cmd         []string
	Labels      map[string]string
	Network     string
	Volume      string // named volume
	MountPath   string
	MemoryBytes int64
	NanoCPUs    int64
	Healthcheck []string
	Entrypoint  []string
	// Publish ContainerPort on PublishHost:PublishPort (local development).
	PublishHost   string
	PublishPort   int
	ContainerPort int
}

func (c *Client) Create(ctx context.Context, s Spec) error {
	host := map[string]any{
		"RestartPolicy": map[string]any{"Name": "no"},
		"Memory":        s.MemoryBytes,
		"MemorySwap":    s.MemoryBytes, // no swap beyond the limit
		"NetworkMode":   s.Network,
		"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "10m", "max-file": "3"}},
	}
	if s.NanoCPUs > 0 {
		host["NanoCpus"] = s.NanoCPUs
	}
	if s.Volume != "" {
		host["Mounts"] = []map[string]any{{"Type": "volume", "Source": s.Volume, "Target": s.MountPath}}
	}
	if s.NetworkMode() == "" {
		host["NetworkMode"] = "bridge"
	}
	body := map[string]any{
		"Image":      s.Image,
		"Env":        s.Env,
		"Labels":     s.Labels,
		"HostConfig": host,
	}
	if s.PublishPort > 0 {
		p := fmt.Sprintf("%d/tcp", s.ContainerPort)
		body["ExposedPorts"] = map[string]any{p: map[string]any{}}
		host["PortBindings"] = map[string]any{p: []map[string]string{{"HostIp": s.PublishHost, "HostPort": fmt.Sprint(s.PublishPort)}}}
	}
	if s.Entrypoint != nil {
		body["Entrypoint"] = s.Entrypoint
	}
	if len(s.Cmd) > 0 {
		body["Cmd"] = s.Cmd
	}
	if len(s.Healthcheck) > 0 {
		body["Healthcheck"] = map[string]any{"Test": s.Healthcheck, "Interval": int64(2 * time.Second), "Timeout": int64(2 * time.Second), "Retries": 30}
	}
	return c.call(ctx, "POST", "/containers/create", url.Values{"name": {s.Name}}, body, nil)
}

func (c *Client) Start(ctx context.Context, name string) error {
	err := c.call(ctx, "POST", "/containers/"+name+"/start", nil, nil, nil)
	if err != nil && strings.Contains(err.Error(), "304") {
		return nil
	}
	return err
}

func (c *Client) Stop(ctx context.Context, name string, timeout time.Duration) error {
	err := c.call(ctx, "POST", "/containers/"+name+"/stop", url.Values{"t": {fmt.Sprint(int(timeout.Seconds()))}}, nil, nil)
	if err != nil && strings.Contains(err.Error(), "304") {
		return nil
	}
	return err
}

func (c *Client) Remove(ctx context.Context, name string) error {
	err := c.call(ctx, "DELETE", "/containers/"+name, url.Values{"force": {"1"}, "v": {"1"}}, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// State is the part of `docker inspect` Sparkbase uses.
type State struct {
	Exists  bool
	Running bool
	Health  string // "", starting, healthy, unhealthy
	Started time.Time
	OOM     bool
}

func (c *Client) Inspect(ctx context.Context, name string) (State, error) {
	var out struct {
		State struct {
			Running   bool      `json:"Running"`
			OOMKilled bool      `json:"OOMKilled"`
			StartedAt time.Time `json:"StartedAt"`
			Health    *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
	}
	err := c.call(ctx, "GET", "/containers/"+name+"/json", nil, nil, &out)
	if errors.Is(err, ErrNotFound) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	st := State{Exists: true, Running: out.State.Running, Started: out.State.StartedAt, OOM: out.State.OOMKilled}
	if out.State.Health != nil {
		st.Health = out.State.Health.Status
	}
	return st, nil
}

// Stats is a one-shot resource sample.
type Stats struct {
	MemoryBytes int64
	MemoryLimit int64
	CPUPercent  float64
}

func (c *Client) Stats(ctx context.Context, name string) (Stats, error) {
	var s struct {
		MemoryStats struct {
			Usage int64            `json:"usage"`
			Limit int64            `json:"limit"`
			Stats map[string]int64 `json:"stats"`
		} `json:"memory_stats"`
		CPUStats struct {
			CPUUsage struct {
				Total int64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System     int64 `json:"system_cpu_usage"`
			OnlineCPUs int64 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				Total int64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System int64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
	}
	if err := c.call(ctx, "GET", "/containers/"+name+"/stats", url.Values{"stream": {"0"}}, nil, &s); err != nil {
		return Stats{}, err
	}
	mem := s.MemoryStats.Usage - s.MemoryStats.Stats["inactive_file"]
	if mem < 0 {
		mem = s.MemoryStats.Usage
	}
	out := Stats{MemoryBytes: mem, MemoryLimit: s.MemoryStats.Limit}
	cpuDelta := float64(s.CPUStats.CPUUsage.Total - s.PreCPUStats.CPUUsage.Total)
	sysDelta := float64(s.CPUStats.System - s.PreCPUStats.System)
	if sysDelta > 0 && cpuDelta > 0 {
		cpus := float64(s.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = 1
		}
		out.CPUPercent = cpuDelta / sysDelta * cpus * 100
	}
	return out, nil
}

// LogLine is one line of container output.
type LogLine struct {
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"` // stdout or stderr
	Text   string    `json:"text"`
}

// Logs streams container output. With follow, it runs until ctx ends.
func (c *Client) Logs(ctx context.Context, name string, tail int, follow bool, fn func(LogLine)) error {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {fmt.Sprint(tail)}}
	if follow {
		q.Set("follow", "1")
	}
	res, err := c.do(ctx, c.stream, "GET", "/containers/"+name+"/logs", q, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// Non-TTY containers multiplex stdout and stderr with an 8-byte header.
	r := bufio.NewReader(res.Body)
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		frame := make([]byte, size)
		if _, err := io.ReadFull(r, frame); err != nil {
			return nil
		}
		stream := "stdout"
		if hdr[0] == 2 {
			stream = "stderr"
		}
		for _, line := range strings.Split(strings.TrimRight(string(frame), "\n"), "\n") {
			ts, text, _ := strings.Cut(line, " ")
			t, _ := time.Parse(time.RFC3339Nano, ts)
			fn(LogLine{Time: t, Stream: stream, Text: text})
		}
	}
}

// Exec runs a command in a running container and returns its output.
func (c *Client) Exec(ctx context.Context, name string, cmd []string) (string, int, error) {
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.call(ctx, "POST", "/containers/"+name+"/exec", nil, map[string]any{"Cmd": cmd, "AttachStdout": true, "AttachStderr": true}, &created); err != nil {
		return "", 0, err
	}
	res, err := c.do(ctx, c.http, "POST", "/exec/"+created.ID+"/start", nil, map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()
	var out strings.Builder
	r := bufio.NewReader(res.Body)
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			break
		}
		frame := make([]byte, binary.BigEndian.Uint32(hdr[4:]))
		if _, err := io.ReadFull(r, frame); err != nil {
			break
		}
		out.Write(frame)
	}
	var info struct {
		ExitCode int `json:"ExitCode"`
	}
	_ = c.call(ctx, "GET", "/exec/"+created.ID+"/json", nil, nil, &info)
	return out.String(), info.ExitCode, nil
}

// ListByLabel returns container names carrying a label.
func (c *Client) ListByLabel(ctx context.Context, label string) ([]string, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {label}})
	var out []struct {
		Names []string `json:"Names"`
	}
	if err := c.call(ctx, "GET", "/containers/json", url.Values{"all": {"1"}, "filters": {string(filters)}}, nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out))
	for _, c := range out {
		if len(c.Names) > 0 {
			names = append(names, strings.TrimPrefix(c.Names[0], "/"))
		}
	}
	return names, nil
}
