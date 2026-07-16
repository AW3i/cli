// Copyright 2025 TechDivision GmbH
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// GlobalConfigDir is the location of the global valet-sh config directory.
const GlobalConfigDir = "/usr/local/valet-sh/etc"

// GlobalConfig represents /usr/local/valet-sh/etc/config.yml.
type GlobalConfig struct {
	HubDomain        string `yaml:"hub_domain,omitempty"`
	HubProject       string `yaml:"hub_project,omitempty"`
	HubGitLabSSHPort int    `yaml:"hub_gitlab_ssh_port,omitempty"`
	DevelopmentTLD   string `yaml:"development_tld,omitempty"`
}

// LoadGlobal reads the global config file. Missing file is not an error —
// the file may not exist on a freshly installed machine.
func LoadGlobal() (*GlobalConfig, error) {
	path := filepath.Join(GlobalConfigDir, "config.yml")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &GlobalConfig{DevelopmentTLD: "test"}, nil
		}
		return nil, fmt.Errorf("reading global config %s: %w", path, err)
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing global config %s: %w", path, err)
	}

	if cfg.DevelopmentTLD == "" {
		cfg.DevelopmentTLD = "test"
	}

	return &cfg, nil
}


