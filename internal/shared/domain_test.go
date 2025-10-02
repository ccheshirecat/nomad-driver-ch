// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package domain

import (
	"testing"

	"github.com/hashicorp/go-multierror"
)

func TestConfig_Validate(t *testing.T) {
	allowedPath := "/allowed/path/"

	validConfig := Config{
		Name:      "test-domain",
		CPUs:      2,
		Memory:    600,
		BaseImage: allowedPath + "image.qcow2",
		OsVariant: &OSVariant{
			Arch:    "x86_64",
			Machine: "pc-i440fx-2.9",
		},
	}

	tests := []struct {
		name         string
		config       Config
		allowedPaths []string
		wantErr      error
	}{
		{
			name:         "Valid_configuration",
			config:       validConfig,
			allowedPaths: []string{allowedPath},
			wantErr:      nil,
		},
		{
			name: "Image_path_not_alloweds",
			config: Config{
				Name:      validConfig.Name,
				Memory:    validConfig.Memory,
				CPUs:      validConfig.CPUs,
				BaseImage: "/path/not/allowed/image.qcow2",
				OsVariant: validConfig.OsVariant,
			},
			allowedPaths: []string{allowedPath},
			wantErr:      multierror.Append(nil, ErrPathNotAllowed),
		},
		{
			name: "User_specific_paths",
			config: Config{
				Name:      "test",
				BaseImage: "/root/my-image.qcow2",
				OsVariant: validConfig.OsVariant,
				Memory:    validConfig.Memory,
				CPUs:      validConfig.CPUs,
			},
			allowedPaths: []string{"/root", "/var/lib/viper"},
			wantErr:      nil, // Should be allowed
		},
		{
			name: "User_specific_paths_not_allowed",
			config: Config{
				Name:      "test",
				BaseImage: "/opt/my-image.qcow2", // Not in allowed paths
				OsVariant: validConfig.OsVariant,
				Memory:    validConfig.Memory,
				CPUs:      validConfig.CPUs,
			},
			allowedPaths: []string{"/root", "/var/lib/viper"},
			wantErr:      multierror.Append(nil, ErrPathNotAllowed),
		},
		{
			name: "Missing_domain_name",
			config: Config{
				Memory:    validConfig.Memory,
				CPUs:      validConfig.CPUs,
				BaseImage: validConfig.BaseImage,
				OsVariant: validConfig.OsVariant,
			},
			allowedPaths: []string{allowedPath},
			wantErr:      multierror.Append(nil, ErrEmptyName),
		},
		{
			name: "Missing_base_image",
			config: Config{
				Name:      validConfig.Name,
				Memory:    validConfig.Memory,
				CPUs:      validConfig.CPUs,
				OsVariant: validConfig.OsVariant,
			},
			allowedPaths: []string{allowedPath},
			wantErr:      multierror.Append(nil, ErrMissingImage),
		},
		{
			name: "Not_enough_memory",
			config: Config{
				Name:      validConfig.Name,
				Memory:    2,
				CPUs:      validConfig.CPUs,
				BaseImage: validConfig.BaseImage,
				OsVariant: validConfig.OsVariant,
			},
			allowedPaths: []string{allowedPath},
			wantErr:      multierror.Append(nil, ErrNotEnoughMemory),
		},
		{
			name: "No_cpus_assigned",
			config: Config{
				Name:      validConfig.Name,
				Memory:    validConfig.Memory,
				CPUs:      0,
				BaseImage: validConfig.BaseImage,
				OsVariant: validConfig.OsVariant,
			},
			allowedPaths: []string{allowedPath},
			wantErr:      multierror.Append(nil, ErrNoCPUS),
		},
		{
			name: "Incomplete_OS_variant",
			config: Config{
				Name:      validConfig.Name,
				Memory:    validConfig.Memory,
				CPUs:      validConfig.CPUs,
				BaseImage: validConfig.BaseImage,
				OsVariant: &OSVariant{
					Arch:    "",
					Machine: "",
				},
			},
			allowedPaths: []string{allowedPath},
			wantErr:      multierror.Append(nil, ErrIncompleteOSVariant),
		},
		{
			name: "All_errors",
			config: Config{
				OsVariant: &OSVariant{
					Arch:    "",
					Machine: "",
				},
			},
			allowedPaths: []string{allowedPath},
			wantErr: multierror.Append(nil, ErrEmptyName, ErrMissingImage,
				ErrNotEnoughMemory, ErrIncompleteOSVariant,
				ErrNoCPUS),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			err := tt.config.Validate(tt.allowedPaths)
			if err != nil && tt.wantErr == nil {
				t.Errorf("expected no error, got %v", err)
			} else if err == nil && tt.wantErr != nil {
				t.Errorf("expected error, got none")
			} else if err != nil && tt.wantErr != nil && err.Error() != tt.wantErr.Error() {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}
