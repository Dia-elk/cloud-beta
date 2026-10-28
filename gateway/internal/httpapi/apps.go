package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloudbeta/gateway/internal/apperr"
	"cloudbeta/gateway/internal/db"
	"cloudbeta/gateway/internal/deploy"

	"github.com/jackc/pgx/v5"
)

type App struct {
	ID          string `json:"id"`
	CompanyID   string `json:"-"`
	Name        string `json:"name"`
	Repo        string `json:"repo"`
	URL         string `json:"url"`
	Status      string `json:"status"`
	ContainerID string `json:"container_id"`
	Port        int    `json:"port"`
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !s.allow(w, r, start, "apps", "read", "read", "") {
		return
	}
	var out []App
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id,name,repo,url,status,container_id,port FROM apps ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (App, error) {
			var a App
			err := row.Scan(&a.ID, &a.Name, &a.Repo, &a.URL, &a.Status, &a.ContainerID, &a.Port)
			return a, err
		})
		return err
	})
	if err != nil {
		dbError(w, "list apps", err)
		return
	}
	if out == nil {
		out = []App{}
	}
	s.audit(r, start, "read", "apps", "", session(r).CompanyID, true, "")
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !s.allow(w, r, start, "apps", "write", "create", "") {
		return
	}
	var in struct {
		Name string `json:"name"`
		Repo string `json:"repo"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Name = strings.TrimSpace(in.Name); in.Name == "" {
		fail(w, apperr.AppNameRequired)
		return
	}
	if in.Repo == "" {
		in.Repo = "sample-app"
	}
	sess := session(r)
	id := db.NewID()
	if err := s.tenant(r.Context(), sess, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `INSERT INTO apps(id,company_id,name,repo,status) VALUES ($1,$2,$3,$4,'registered')`, id, sess.CompanyID, in.Name, in.Repo)
		return err
	}); err != nil {
		dbError(w, "create app", err)
		return
	}
	s.audit(r, start, "create", "apps", id, sess.CompanyID, true, "")
	writeJSON(w, http.StatusCreated, App{ID: id, Name: in.Name, Repo: in.Repo, Status: "registered"})
}

// appRow looks an app up across tenants (owner pool) so a cross-tenant
// attempt can be told apart from a missing app, denied and audited.
func (s *Server) appRow(w http.ResponseWriter, r *http.Request, id string) (App, bool) {
	a := App{ID: id}
	err := s.DB.Owner.QueryRow(r.Context(),
		`SELECT company_id,name,repo,url,status,container_id,port FROM apps WHERE id=$1`, id).
		Scan(&a.CompanyID, &a.Name, &a.Repo, &a.URL, &a.Status, &a.ContainerID, &a.Port)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.AppNotFound)
		return a, false
	}
	if err != nil {
		dbError(w, "app lookup", err)
		return a, false
	}
	return a, true
}

func (s *Server) deployer() (deploy.Deployer, error) {
	if s.Deployer != nil {
		return s.Deployer, nil
	}
	return deploy.NewFromEnv()
}

// publicAppURL is the URL reported for a deployed app. The host part is
// deployment configuration, never taken from the request.
func (s *Server) publicAppURL(port int) string {
	return s.Cfg.DeployPublicHost + ":" + strconv.Itoa(port)
}

// appHealthy checks the public URL, then the port on localhost, then on
// DeployProbeHost. A gateway in a container cannot reach host-published
// ports via its own localhost, so compose sets the probe host to
// http://host.docker.internal. Any 2xx counts; no check means unhealthy.
func (s *Server) appHealthy(port int) bool {
	if port <= 0 {
		return false
	}
	candidates := []string{s.publicAppURL(port), "http://localhost:" + strconv.Itoa(port)}
	if p := s.Cfg.DeployProbeHost; p != "" {
		if !strings.HasPrefix(p, "http://") && !strings.HasPrefix(p, "https://") {
			p = "http://" + p
		}
		candidates = append(candidates, strings.TrimSuffix(p, "/")+":"+strconv.Itoa(port))
	}
	seen := map[string]bool{}
	for _, u := range candidates {
		if !seen[u] && deploy.CheckURL(u) {
			return true
		}
		seen[u] = true
	}
	return false
}

// isTemplateRepo reports whether a repo can be deployed today. Only the
// Client Tracker template is real; git deploy returns APP_003 and the
// app is never marked deployed.
func isTemplateRepo(repo string) bool {
	return repo == "" || repo == "sample-app" || repo == "template:client-tracker"
}

func (s *Server) setAppStatus(r *http.Request, id, status string) {
	_ = s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE apps SET status=$2 WHERE id=$1`, id, status)
		return err
	})
}

func (s *Server) handleAppDeploy(w http.ResponseWriter, r *http.Request) {
	start, id, sess := time.Now(), r.PathValue("id"), session(r)
	if !s.allow(w, r, start, "apps", "write", "deploy", id) {
		return
	}
	a, ok := s.appRow(w, r, id)
	if !ok || !s.ownedBy(w, r, start, a.CompanyID, "deploy", "apps", id) {
		return
	}
	if !isTemplateRepo(a.Repo) {
		fail(w, apperr.AppGitDeployDisabled)
		return
	}
	dep, err := s.deployer()
	if err != nil {
		s.setAppStatus(r, id, "failed")
		s.audit(r, start, "deploy", "apps", id, sess.CompanyID, false, "docker-unavailable")
		fail(w, apperr.GatewayDockerDown)
		return
	}
	s.setAppStatus(r, id, "deploying")
	// Redeploy: clear any previous container first (best effort).
	if a.ContainerID != "" {
		_ = dep.Stop(a.ContainerID)
		_ = dep.Remove(a.ContainerID)
	}
	containerID, port, _, err := dep.Start(id, sess.CompanyID, s.Cfg.DeployImage)
	if err != nil {
		s.setAppStatus(r, id, "failed")
		s.audit(r, start, "deploy", "apps", id, sess.CompanyID, false, "deploy failed: "+err.Error())
		if errors.Is(err, deploy.ErrDockerUnavailable) {
			fail(w, apperr.GatewayDockerDown)
			return
		}
		fail(w, apperr.AppDeployFailed)
		return
	}
	healthy := false
	for i := 0; i < 15 && !healthy; i++ {
		if healthy = s.appHealthy(port); !healthy {
			time.Sleep(time.Second)
		}
	}
	if !healthy {
		_ = dep.Stop(containerID)
		_ = dep.Remove(containerID)
		_ = s.tenant(r.Context(), sess, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `UPDATE apps SET status='failed', container_id='', port=0 WHERE id=$1`, id)
			return err
		})
		s.audit(r, start, "deploy", "apps", id, sess.CompanyID, false, "deployed container never became healthy")
		fail(w, apperr.AppUnhealthy)
		return
	}
	url := s.publicAppURL(port)
	if err := s.tenant(r.Context(), sess, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE apps SET status='running', url=$2, container_id=$3, port=$4 WHERE id=$1`, id, url, containerID, port)
		return err
	}); err != nil {
		// The container runs but the app row does not record it; do not
		// report a running app the control plane cannot track.
		_ = dep.Stop(containerID)
		_ = dep.Remove(containerID)
		dbError(w, "deploy record running", err)
		return
	}
	s.audit(r, start, "deploy", "apps", id, sess.CompanyID, true, "real container deployed")
	a.Status, a.URL, a.ContainerID, a.Port = "running", url, containerID, port
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleAppStop(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	if !s.allow(w, r, start, "apps", "write", "stop", id) {
		return
	}
	a, ok := s.appRow(w, r, id)
	if !ok || !s.ownedBy(w, r, start, a.CompanyID, "stop", "apps", id) {
		return
	}
	if a.ContainerID != "" {
		if dep, err := s.deployer(); err == nil {
			_ = dep.Stop(a.ContainerID)
		}
	}
	if err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE apps SET status='stopped' WHERE id=$1`, id)
		return err
	}); err != nil {
		dbError(w, "stop app", err)
		return
	}
	s.audit(r, start, "stop", "apps", id, a.CompanyID, true, "")
	a.Status = "stopped"
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleAppDelete(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	if !s.allow(w, r, start, "apps", "delete", "delete", id) {
		return
	}
	a, ok := s.appRow(w, r, id)
	if !ok || !s.ownedBy(w, r, start, a.CompanyID, "delete", "apps", id) {
		return
	}
	if a.ContainerID != "" {
		if dep, err := s.deployer(); err == nil {
			_ = dep.Stop(a.ContainerID)
			_ = dep.Remove(a.ContainerID)
		}
	}
	// Delete via the tenant transaction, so the RLS owner-only delete
	// policy still applies.
	var affected int64
	if err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		cmd, err := tx.Exec(r.Context(), `DELETE FROM apps WHERE id=$1`, id)
		affected = cmd.RowsAffected()
		return err
	}); err != nil {
		dbError(w, "delete app", err)
		return
	}
	if affected == 0 {
		fail(w, apperr.AppNotFound)
		return
	}
	s.audit(r, start, "delete", "apps", id, a.CompanyID, true, "")
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) handleAppHealth(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	if !s.allow(w, r, start, "apps", "read", "read", id) {
		return
	}
	a, ok := s.appRow(w, r, id)
	if !ok || !s.ownedBy(w, r, start, a.CompanyID, "read", "apps", id) {
		return
	}
	containerStatus := "not-found"
	if a.ContainerID != "" {
		containerStatus = "unknown"
		if dep, err := s.deployer(); err == nil {
			if st, err := dep.InspectStatus(a.ContainerID); err == nil {
				containerStatus = st
			}
		}
	}
	// Healthy only from a live HTTP check on the stored port.
	healthy := a.Status == "running" && s.appHealthy(a.Port)
	if a.URL == "" && a.Port > 0 {
		a.URL = s.publicAppURL(a.Port)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": a.Status, "container_status": containerStatus, "url": a.URL, "healthy": healthy})
}
