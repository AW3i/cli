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
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProject(t *testing.T) {
	// Create a temp directory with a valid .valet-sh.yml
	tmpDir := t.TempDir()
	configContent := `
hub:
  host: "git.example.com"
  port: 22
  path: "/data"
services:
  php:
    version: 8.1
  mariadb:
    version: 10.6
    database: testdb
instance:
  key: "testproject"
  type: "magento2"
  path: "src"
`
	configPath := filepath.Join(tmpDir, ProjectFileName)
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadProject(tmpDir)
	if err != nil {
		t.Fatalf("LoadProject failed: %v", err)
	}

	if cfg.Hub.Host != "git.example.com" {
		t.Errorf("Hub.Host = %q, want git.example.com", cfg.Hub.Host)
	}
	if cfg.Hub.Port != 22 {
		t.Errorf("Hub.Port = %d, want 22", cfg.Hub.Port)
	}
	if cfg.Services.PHP == nil || cfg.Services.PHP.Version != 8.1 {
		t.Errorf("Expected PHP version 8.1, got %v", cfg.Services.PHP.Version)
	}
	if cfg.Services.MariaDB == nil || cfg.Services.MariaDB.Version != 10.6 {
		t.Errorf("Expected MariaDB version 10.6, got %v", cfg.Services.MariaDB.Version)
	}
	if cfg.Instance.Key != "testproject" {
		t.Errorf("Instance.Key = %q, want testproject", cfg.Instance.Key)
	}
}

func TestLoadProjectNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := LoadProject(tmpDir)
	if err == nil {
		t.Error("Expected error for missing config file")
	}
}

func TestLoadProjectInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ProjectFileName)
	os.WriteFile(configPath, []byte("not: valid: yaml: ["), 0o644)

	_, err := LoadProject(tmpDir)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		config   ProjectConfig
		wantErrs int
	}{
		{
			name: "valid magento2",
			config: ProjectConfig{
				Services: ServicesConfig{
					PHP:     &PHPService{Version: "8.1"},
					MariaDB: &MariaDBService{Version: "10.6"},
				},
				Instance: InstanceConfig{
					Key:  "myproject",
					Type: "magento2",
				},
			},
			wantErrs: 0,
		},
		{
			name: "missing key",
			config: ProjectConfig{
				Instance: InstanceConfig{
					Type: "magento2",
				},
			},
			wantErrs: 1,
		},
		{
			name: "missing type",
			config: ProjectConfig{
				Instance: InstanceConfig{
					Key: "myproject",
				},
			},
			wantErrs: 1,
		},
		{
			name: "invalid type",
			config: ProjectConfig{
				Instance: InstanceConfig{
					Key:  "myproject",
					Type: "invalid",
				},
			},
			wantErrs: 1,
		},
		{
			name: "mysql and mariadb both set",
			config: ProjectConfig{
				Services: ServicesConfig{
					MySQL:   &MySQLService{Version: "8.0"},
					MariaDB: &MariaDBService{Version: "10.6"},
				},
				Instance: InstanceConfig{
					Key:  "myproject",
					Type: "magento2",
				},
			},
			wantErrs: 1,
		},
		{
			name: "elasticsearch and opensearch both set",
			config: ProjectConfig{
				Services: ServicesConfig{
					Elasticsearch: &ElasticsearchService{Version: "7"},
					OpenSearch:    &OpenSearchService{Version: "2"},
				},
				Instance: InstanceConfig{
					Key:  "myproject",
					Type: "magento2",
				},
			},
			wantErrs: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.config.Validate()
			if len(errs) != tc.wantErrs {
				t.Errorf("Validate() returned %d errors, want %d: %v", len(errs), tc.wantErrs, errs)
			}
		})
	}
}
