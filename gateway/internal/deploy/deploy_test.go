package deploy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeDocker is a minimal Docker API over HTTP for tests.
type fakeDocker struct {
	mu      sync.Mutex
	created []string // container names, in creation order
	started []string
	stopped []string
	removed []string
	// usedPort simulates a container already bound to this host port.
	usedPort int
	// failStartWithBind makes the first start fail with a bind conflict.
	failStartWithBind bool
	startCalls        int
	status            map[string]string // container id -> status
}

func (f *fakeDocker) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/containers/json":
		w.Header().Set("Content-Type", "application/json")
		if f.usedPort != 0 {
			fmt.Fprintf(w, `[{"Id":"existing","HostConfig":{"PortBindings":{"3000/tcp":[{"HostPort":"%d"}]}}}]`, f.usedPort)
		} else {
			fmt.Fprint(w, `[]`)
		}
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/containers/create"):
		name := r.URL.Query().Get("name")
		f.created = append(f.created, name)
		f.status["ctr_"+name] = "created"
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"Id":"ctr_%s"}`, name)
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/start"):
		f.startCalls++
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/start")
		if f.failStartWithBind && f.startCalls == 1 {
			w.WriteHeader(500)
			fmt.Fprint(w, `{"message":"driver failed programming external connectivity: port is already allocated"}`)
			return
		}
		f.started = append(f.started, id)
		f.status[id] = "running"
		w.WriteHeader(204)
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/stop"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/stop")
		f.stopped = append(f.stopped, id)
		f.status[id] = "exited"
		w.WriteHeader(204)
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/containers/"):
		id := strings.TrimPrefix(r.URL.Path, "/containers/")
		f.removed = append(f.removed, id)
		delete(f.status, id)
		w.WriteHeader(204)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/containers/"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/json")
		st, ok := f.status[id]
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"no such container"}`)
			return
		}
		running := st == "running"
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"Id":%q,"State":{"Status":%q,"Running":%v}}`, id, st, running)
	default:
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message":"not found"}`)
	}
}

func newTestClient(t *testing.T, f *fakeDocker) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestPortAllocationSkipsUsedPorts(t *testing.T) {
	t.Setenv("DEPLOY_PORT_MIN", "3100")
	t.Setenv("DEPLOY_PORT_MAX", "3102")
	f := &fakeDocker{status: map[string]string{}, usedPort: 3100}
	c := newTestClient(t, f)
	_, port, _, err := c.Start("app_1", "cmp_1", "cloudbeta-sample-app")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if port != 3101 {
		t.Fatalf("expected first free port 3101 (3100 in use), got %d", port)
	}
}

func TestCreateAndStartSuccess(t *testing.T) {
	t.Setenv("DEPLOY_PORT_MIN", "3100")
	t.Setenv("DEPLOY_PORT_MAX", "3199")
	f := &fakeDocker{status: map[string]string{}}
	c := newTestClient(t, f)
	id, port, url, err := c.Start("app_abc", "cmp_1", "cloudbeta-sample-app")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" || port < 3100 || port > 3199 {
		t.Fatalf("bad start result: id=%q port=%d", id, port)
	}
	if url != fmt.Sprintf("http://localhost:%d", port) {
		t.Fatalf("bad url: %q", url)
	}
	if len(f.created) != 1 || f.created[0] != "cloudbeta-app_abc" {
		t.Fatalf("expected container name cloudbeta-app_abc, got %v", f.created)
	}
	if len(f.started) != 1 {
		t.Fatalf("expected container started once, got %v", f.started)
	}
	st, err := c.InspectStatus(id)
	if err != nil || st != "running" {
		t.Fatalf("InspectStatus: %q, %v", st, err)
	}
}

func TestStartFailureSurfacesErrorNoFakeSuccess(t *testing.T) {
	t.Setenv("DEPLOY_PORT_MIN", "3100")
	t.Setenv("DEPLOY_PORT_MAX", "3100")
	// Every port is taken and the daemon refuses: Start must error.
	f := &fakeDocker{status: map[string]string{}, usedPort: 3100}
	c := newTestClient(t, f)
	_, _, _, err := c.Start("app_x", "cmp_1", "cloudbeta-sample-app")
	if err == nil {
		t.Fatal("expected Start to fail when no port is free, got success")
	}
}

func TestStartRetriesNextPortOnBindConflict(t *testing.T) {
	t.Setenv("DEPLOY_PORT_MIN", "3100")
	t.Setenv("DEPLOY_PORT_MAX", "3102")
	f := &fakeDocker{status: map[string]string{}, failStartWithBind: true}
	c := newTestClient(t, f)
	_, port, _, err := c.Start("app_retry", "cmp_1", "cloudbeta-sample-app")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if port != 3101 {
		t.Fatalf("expected retry on next port 3101 after bind conflict, got %d", port)
	}
	if len(f.removed) == 0 {
		t.Fatal("expected failed container to be removed on bind-conflict retry")
	}
}

func TestStopRemoveCalls(t *testing.T) {
	f := &fakeDocker{status: map[string]string{"ctr_1": "running"}}
	c := newTestClient(t, f)
	if err := c.Stop("ctr_1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(f.stopped) != 1 || f.stopped[0] != "ctr_1" {
		t.Fatalf("stop not called: %v", f.stopped)
	}
	if err := c.Remove("ctr_1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(f.removed) != 1 {
		t.Fatalf("remove not called: %v", f.removed)
	}
	st, err := c.InspectStatus("ctr_1")
	if err != nil || st != "not-found" {
		t.Fatalf("after remove, InspectStatus: %q, %v", st, err)
	}
	// Stop/Remove of a missing container are idempotent, not errors.
	if err := c.Stop("ctr_missing"); err != nil {
		t.Fatalf("Stop missing: %v", err)
	}
	if err := c.Remove("ctr_missing"); err != nil {
		t.Fatalf("Remove missing: %v", err)
	}
}

func TestHealthCheckTrueFalse(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "hello")
	}))
	defer ok.Close()
	if !CheckURL(ok.URL) {
		t.Fatal("expected healthy for 200")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer bad.Close()
	if CheckURL(bad.URL) {
		t.Fatal("expected unhealthy for 500")
	}
	// Unreachable host: unhealthy, never an error panic.
	if CheckURL("http://127.0.0.1:1/") {
		t.Fatal("expected unhealthy for refused connection")
	}
}

func TestDockerUnavailable(t *testing.T) {
	// A unix socket that does not exist must fail fast, not silently.
	_, err := New("unix:///tmp/cloudbeta-no-such-docker.sock")
	if err == nil {
		t.Fatal("expected error for missing docker socket")
	}
	if !strings.Contains(err.Error(), "docker-unavailable") {
		t.Fatalf("expected docker-unavailable error, got %v", err)
	}
}

func TestFirstFree(t *testing.T) {
	used := map[int]bool{3100: true, 3101: true}
	p, err := firstFree(used, 3100, 3102)
	if err != nil || p != 3102 {
		t.Fatalf("firstFree: %d, %v", p, err)
	}
	if _, err := firstFree(map[int]bool{3100: true}, 3100, 3100); err == nil {
		t.Fatal("expected error when range exhausted")
	}
}
