/*
 * distrohop - A utility for correlating and identifying equivalent software
 * packages across different Linux distributions
 *
 * Copyright (C) 2025 Elara Ivy <elara@elara.ws>
 *
 * This file is part of distrohop.
 *
 * distrohop is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * distrohop is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with distrohop.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/go-co-op/gocron/v2"
	"go.elara.ws/distrohop/internal/config"
	"go.elara.ws/distrohop/internal/index"
	"go.elara.ws/distrohop/internal/pull"
	"go.elara.ws/distrohop/internal/store"
	"go.elara.ws/distrohop/internal/store/cached"
	"go.elara.ws/distrohop/internal/store/combined"
	"go.elara.ws/loggers"
	"go.elara.ws/salix"
)

//go:embed templates
var tmpls embed.FS

//go:embed assets
var assets embed.FS

func main() {
	log := slog.New(loggers.NewPretty(os.Stderr, loggers.Options{Level: slog.LevelDebug}))
	cfg, err := config.Load()
	if err != nil {
		log.Error("Error loading configuration", slog.Any("error", err))
		os.Exit(1)
	}

	dataDir, err := userDataDir()
	if err != nil {
		log.Error("Error getting data directory", slog.Any("error", err))
		os.Exit(1)
	}
	dataDir = filepath.Join(dataDir, "distrohop")

	stores := map[string]store.ReadOnly{}

	// Create a scheduler for repo refresh tasks
	sched, err := gocron.NewScheduler(
		gocron.WithLocation(time.Local),
	)
	if err != nil {
		log.Error("Error creating scheduler", slog.Any("error", err))
		os.Exit(1)
	}
	sched.Start()
	defer sched.Shutdown()

	for _, repo := range cfg.Repos {
		// Create a combined store for the repo
		cs := combined.New()
		// Create a cached store for the combined store
		stores[repo.Name] = cached.New(cs, time.Hour, 10*time.Minute)

		for _, repoName := range repo.Repos {
			for _, arch := range repo.Architectures {
				dbPath := filepath.Join(dataDir, repo.Name, repo.Version, repoName, arch, "db")
				// Open a store for a specific index within a repo
				s, err := store.Open(dbPath)
				if err == nil {
					// Add the index store to the combined store for the repo
					cs.Add(s)
				} else if err != nil {
					log.Error("Error opening database", slog.Any("error", err))
					os.Exit(1)
				}

				// Schedule a refresh job for the repo
				if job := scheduleRefresh(log, s, sched, repo, repoName, arch); job != nil {
					// Run the refresh job immediately on startup
					if err := job.RunNow(); err != nil {
						log.Warn("Error executing repo refresh task on startup", slog.String("repo", repoName), slog.Any("error", err))
					}
				}
			}
		}
	}

	tmplFS, err := fs.Sub(tmpls, "templates")
	if err != nil {
		log.Error("Error getting templates subdirectory", slog.Any("error", err))
		os.Exit(1)
	}

	ns := salix.New().
		WithEscapeHTML(true).
		WithWriteOnSuccess(true).
		WithTagMap(map[string]salix.Tag{
			"icon": salix.FSTag{
				FS:         assets,
				PathPrefix: "assets/icons",
				Extension:  ".svg",
			},
		}).
		WithVarMap(map[string]any{
			"sprintf": fmt.Sprintf,
		})

	err = ns.ParseFSGlob(tmplFS, "*")
	if err != nil {
		log.Error("Error parsing templates", slog.Any("error", err))
		os.Exit(1)
	}

	mux := chi.NewMux()

	mux.Handle("/assets/*", http.FileServer(http.FS(assets)))

	mux.Get("/", handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
		return ns.ExecuteTemplate(w, "home.html", map[string]any{"cfg": cfg})
	}))

	mux.Get("/about", handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
		return ns.ExecuteTemplate(w, "about.html", nil)
	}))

	mux.Get("/pkg/{repo}/{package}", handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
		repo := chi.URLParam(r, "repo")
		s, ok := stores[repo]
		if !ok {
			return fmt.Errorf("no such repo: %q", repo)
		}

		pkgName := chi.URLParam(r, "package")
		pkg, err := s.GetPkg(pkgName)
		if errors.Is(err, pebble.ErrNotFound) {
			return fmt.Errorf("no such package: %q", pkgName)
		} else if err != nil {
			return err
		}

		return ns.ExecuteTemplate(w, "package.html", map[string]any{
			"inRepo": repo,
			"pkg":    pkg,
		})
	}))

	mux.Handle("/suggestions", handleErrJSON(func(w http.ResponseWriter, r *http.Request) error {
		if r.Method != http.MethodGet {
			return httpError{fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed}
		}

		repo := r.URL.Query().Get("repo")
		s, ok := stores[repo]
		if !ok {
			return httpError{fmt.Errorf("no such repo: %q", repo), http.StatusNotFound}
		}

		pkgs, err := s.GetPkgNamesByPrefix(r.URL.Query().Get("input"), 10)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(pkgs)
	}))

	limiter := httprate.Limit(
		10,
		10*time.Second,
		httprate.WithKeyFuncs(httprate.KeyByRealIP),
		httprate.WithLimitHandler(handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
			return httpError{errors.New("You've made too many requests. Please try again later."), http.StatusTooManyRequests}
		})),
	)

	mux.With(limiter).Route("/search", func(search chi.Router) {
		search.Get("/tags", handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
			query := r.URL.Query()
			tags := query["tag"]

			inRepo := query.Get("in")
			in, ok := stores[inRepo]
			if !ok {
				return httpError{fmt.Errorf("no such repo: %q", inRepo), http.StatusNotFound}
			}

			results, latency, err := in.Search(tags)
			if errors.Is(err, store.ErrInvalidTag) {
				return httpError{err, http.StatusBadRequest}
			} else if err != nil {
				return err
			}

			return ns.ExecuteTemplate(w, "results.html", map[string]any{
				"results":  results,
				"fromRepo": "",
				"inRepo":   inRepo,
				"tags":     tags,
				"procTime": latency,
			})
		}))

		search.Get("/pkg", handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
			query := r.URL.Query()

			inRepo := query.Get("in")
			in, ok := stores[inRepo]
			if !ok {
				return httpError{fmt.Errorf("no such repo: %q", inRepo), http.StatusNotFound}
			}

			fromRepo := query.Get("from")
			from, ok := stores[fromRepo]
			if !ok {
				return httpError{fmt.Errorf("no such repo: %q", fromRepo), http.StatusNotFound}
			}

			pkgName := query.Get("pkg")
			pkg, err := from.GetPkg(pkgName)
			if err != nil {
				return err
			}

			results, latency, err := in.Search(pkg.Tags)
			if err != nil {
				return err
			}

			return ns.ExecuteTemplate(w, "results.html", map[string]any{
				"results":  results,
				"fromRepo": fromRepo,
				"inRepo":   inRepo,
				"pkgName":  pkgName,
				"procTime": latency,
			})
		}))
	})

	mux.NotFound(handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
		return httpError{errors.New("page not found"), http.StatusNotFound}
	}))

	mux.MethodNotAllowed(handleErrGUI(ns, func(w http.ResponseWriter, r *http.Request) error {
		return httpError{fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed}
	}))

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go handleShutdown(ch, log, srv, sched)

	log.Info("Starting HTTP server", slog.Int("port", 8080))
	srv.ListenAndServe()
}

// handleShutdown handles a shutdown signal, such as an OS interrupt
func handleShutdown(ch chan os.Signal, log *slog.Logger, srv *http.Server, sched gocron.Scheduler) {
	sig := <-ch
	log.Info("Shutting down server", slog.String("signal", sig.String()))
	srv.Shutdown(nil)
	sched.Shutdown()
}

// scheduleRefresh schedules a job to refresh a repo index database
func scheduleRefresh(log *slog.Logger, s *store.Store, sched gocron.Scheduler, repo config.Repo, repoName, arch string) (job gocron.Job) {
	var err error
	job, err = sched.NewJob(
		gocron.CronJob(repo.RefreshSchedule, true),
		gocron.NewTask(func() {
			opts := pull.Options{
				BaseURL:      repo.BaseURL,
				Version:      repo.Version,
				Repo:         repoName,
				Architecture: arch,
				ProgressFunc: func(title string, received, total int64) {
					log.Debug(
						fmt.Sprintf("[%s] download", title),
						slog.Int64("recvd", received),
						slog.Int64("total", total),
					)
				},
			}

			importer, err := index.GetImporter(repo.Type)
			if err != nil {
				log.Error("Error getting importer", slog.Any("error", err))
				return
			}

			log.Info(
				"Pulling repo",
				slog.String("name", repo.Name),
				slog.String("version", repo.Version),
				slog.String("repo", repoName),
				slog.String("arch", arch),
			)

			err = pull.Pull(opts, s, importer)
			if err != nil && !errors.Is(err, pull.ErrUpToDate) {
				log.Warn("Error pulling repository", slog.String("repo", repoName), slog.Any("error", err))
			}

			nextRun, err := job.NextRun()
			if err != nil {
				return
			}

			log.Info(
				fmt.Sprintf(
					"Next refresh scheduled for %s",
					nextRun.Format(time.RFC1123),
				),
				slog.String("name", repo.Name),
				slog.String("version", repo.Version),
				slog.String("repo", repoName),
				slog.String("arch", arch),
			)
		}),
	)
	if err != nil {
		log.Warn("Error scheduling repo refresh task", slog.String("repo", repoName), slog.Any("error", err))
	}
	return job
}

// userDataDir returns the directory where distrohop should store its indices
func userDataDir() (string, error) {
	if os.Getenv("RUNNING_IN_DOCKER") == "true" {
		return "/data", nil
	}
	if dir, ok := os.LookupEnv("XDG_DATA_HOME"); ok {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/share"), nil
}
