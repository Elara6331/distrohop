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

package config

import (
	"os"
	"path/filepath"

	"github.com/caarlos0/env/v11"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	SearchThreads int    `toml:"searchThreads" env:"SEARCH_THREADS"`
	Repos         []Repo `toml:"repo" envPrefix:"REPO"`
}

type Repo struct {
	Name            string   `toml:"name" env:"NAME"`
	Type            string   `toml:"type" env:"TYPE"`
	BaseURL         string   `toml:"base_url" env:"BASE_URL"`
	Version         string   `toml:"version" env:"VERSION"`
	Repos           []string `toml:"repos" env:"REPOS"`
	Architectures   []string `toml:"arch" env:"ARCHES"`
	RefreshSchedule string   `toml:"refresh_schedule" env:"REFRESH_SCHEDULE"`
}

func Load() (cfg *Config, err error) {
	cfg = &Config{
		SearchThreads: 4,
	}

	if fl, err := os.Open("/etc/distrohop.toml"); err == nil {
		err = toml.NewDecoder(fl).Decode(cfg)
		if err != nil {
			return nil, err
		}
	}

	cfgDir := "/distrohop.toml"
	if os.Getenv("RUNNING_IN_DOCKER") != "true" {
		cfgDir, err = os.UserConfigDir()
		if err != nil {
			return nil, err
		}
	}

	if fl, err := os.Open(filepath.Join(cfgDir, "distrohop.toml")); err == nil {
		err = toml.NewDecoder(fl).Decode(cfg)
		if err != nil {
			return nil, err
		}
	}

	err = env.ParseWithOptions(cfg, env.Options{Prefix: "DISTROHOP_"})
	if err != nil {
		return nil, err
	}

	for i, repo := range cfg.Repos {
		if len(repo.Architectures) == 0 {
			repo.Architectures = []string{""}
		}
		if len(repo.Repos) == 0 {
			repo.Repos = []string{""}
		}
		if repo.RefreshSchedule == "" {
			repo.RefreshSchedule = "0 0 * * *"
		}
		cfg.Repos[i] = repo
	}

	return cfg, nil
}
