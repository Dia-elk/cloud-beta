// Gateway: control plane + permission gateway. Loads the policy,
// connects both database pools (failing hard if the tenant pool could
// bypass RLS), and serves the HTTP API from internal/httpapi.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloudbeta/gateway/internal/db"
	"cloudbeta/gateway/internal/deploy"
	"cloudbeta/gateway/internal/httpapi"
	"cloudbeta/gateway/internal/policy"
)

func main() {
	// `gateway healthcheck` is the container healthcheck: the image is
	// FROM scratch, so there is no curl or wget to probe /health with.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	cfg := httpapi.ConfigFromEnv()
	if len(cfg.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must be set to at least 32 characters, refusing to start")
	}
	pol, err := loadPolicy()
	if err != nil {
		log.Fatalf("load policy: %v", err)
	}
	ctx := context.Background()
	d, err := db.Connect(ctx, os.Getenv("DATABASE_URL"), os.Getenv("APP_DATABASE_URL"), pol)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	s := &httpapi.Server{DB: d, Pol: pol, Cfg: cfg}
	if dep, err := deploy.NewFromEnv(); err != nil {
		// Not fatal: everything except deploy works without Docker;
		// deploy requests then return GATEWAY_005.
		log.Printf("docker deployer unavailable at startup (deploy will return GATEWAY_005): %v", err)
	} else {
		s.Deployer = dep
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Deploy waits up to ~15s for a container health check.
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		log.Printf("gateway listening on :%s (allowed origins %v, secure cookies %v)", port, cfg.AllowedOrigins, cfg.CookieSecure)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	d.Owner.Close()
	d.App.Close()
}

func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func loadPolicy() (*policy.Policy, error) {
	paths := []string{os.Getenv("POLICY_PATH"), "policy.json", "/gateway/policy.json"}
	var lastErr error
	for _, p := range paths {
		if p == "" {
			continue
		}
		pol, err := policy.Load(p)
		if err == nil {
			return pol, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
