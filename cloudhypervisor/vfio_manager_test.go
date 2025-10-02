// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloudhypervisor

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/go-hclog"
)

// MockFileSystem implements FileSystem for testing
type MockFileSystem struct {
	Files     map[string]string
	Symlinks  map[string]string
	Writes    map[string][]byte
	WriteErrs map[string]error
}

func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		Files:     make(map[string]string),
		Symlinks:  make(map[string]string),
		Writes:    make(map[string][]byte),
		WriteErrs: make(map[string]error),
	}
}

func (m *MockFileSystem) ReadFile(path string) ([]byte, error) {
	if content, ok := m.Files[path]; ok {
		return []byte(content), nil
	}
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) WriteFile(path string, data []byte) error {
	if err, ok := m.WriteErrs[path]; ok {
		return err
	}
	m.Writes[path] = data
	return nil
}

func (m *MockFileSystem) ReadDir(path string) ([]os.DirEntry, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockFileSystem) Exists(path string) bool {
	_, fileExists := m.Files[path]
	_, symlinkExists := m.Symlinks[path]
	return fileExists || symlinkExists
}

func (m *MockFileSystem) Readlink(path string) (string, error) {
	if target, ok := m.Symlinks[path]; ok {
		return target, nil
	}
	return "", os.ErrNotExist
}

// Helper to setup a mock device
func (m *MockFileSystem) AddDevice(pciAddr, vendor, device, driver, iommuGroup string) {
	// Device directory exists (this is what Exists() checks)
	m.Files[fmt.Sprintf("/sys/bus/pci/devices/%s", pciAddr)] = ""

	// Device attribute files
	m.Files[fmt.Sprintf("/sys/bus/pci/devices/%s/vendor", pciAddr)] = fmt.Sprintf("0x%s\n", vendor)
	m.Files[fmt.Sprintf("/sys/bus/pci/devices/%s/device", pciAddr)] = fmt.Sprintf("0x%s\n", device)

	// Driver symlink
	if driver != "" {
		m.Symlinks[fmt.Sprintf("/sys/bus/pci/devices/%s/driver", pciAddr)] = fmt.Sprintf("../../../bus/pci/drivers/%s", driver)
	}

	// IOMMU group symlink
	if iommuGroup != "" {
		m.Symlinks[fmt.Sprintf("/sys/bus/pci/devices/%s/iommu_group", pciAddr)] = fmt.Sprintf("../../../../kernel/iommu_groups/%s", iommuGroup)
		m.Files[fmt.Sprintf("/dev/vfio/%s", iommuGroup)] = ""
	}
}

func TestIsValidPCIAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"valid standard", "0000:01:00.0", false},
		{"valid hex upper", "0000:0A:00.1", false},
		{"valid hex lower", "0000:0a:00.2", false},
		{"valid function 7", "0000:01:00.7", false},
		{"invalid function 8", "0000:01:00.8", true},
		{"invalid format short", "01:00.0", true},
		{"invalid format no function", "0000:01:00", true},
		{"invalid chars", "000G:01:00.0", true},
		{"empty string", "", true},
		{"spaces", "0000:01:00.0 ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := isValidPCIAddress(tt.addr)
			if valid == tt.wantErr {
				t.Errorf("isValidPCIAddress(%q) = %v, want %v", tt.addr, valid, !tt.wantErr)
			}
		})
	}
}

func TestMatchesAllowlist(t *testing.T) {
	tests := []struct {
		name      string
		vendor    string
		device    string
		allowlist []string
		want      bool
	}{
		{
			name:      "exact match",
			vendor:    "10de",
			device:    "2204",
			allowlist: []string{"10de:2204"},
			want:      true,
		},
		{
			name:      "wildcard vendor match",
			vendor:    "10de",
			device:    "2204",
			allowlist: []string{"10de:*"},
			want:      true,
		},
		{
			name:      "wildcard wrong vendor",
			vendor:    "8086",
			device:    "0d26",
			allowlist: []string{"10de:*"},
			want:      false,
		},
		{
			name:      "multiple patterns first match",
			vendor:    "10de",
			device:    "2204",
			allowlist: []string{"10de:*", "8086:*"},
			want:      true,
		},
		{
			name:      "multiple patterns second match",
			vendor:    "8086",
			device:    "0d26",
			allowlist: []string{"10de:*", "8086:*"},
			want:      true,
		},
		{
			name:      "not in allowlist",
			vendor:    "1002",
			device:    "67df",
			allowlist: []string{"10de:*", "8086:*"},
			want:      false,
		},
		{
			name:      "empty allowlist",
			vendor:    "10de",
			device:    "2204",
			allowlist: []string{},
			want:      false,
		},
		{
			name:      "case insensitive exact",
			vendor:    "10DE",
			device:    "2204",
			allowlist: []string{"10de:2204"},
			want:      true,
		},
		{
			name:      "case insensitive wildcard",
			vendor:    "10DE",
			device:    "2204",
			allowlist: []string{"10de:*"},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAllowlist(tt.vendor, tt.device, tt.allowlist)
			if got != tt.want {
				t.Errorf("matchesAllowlist(%s, %s, %v) = %v, want %v",
					tt.vendor, tt.device, tt.allowlist, got, tt.want)
			}
		})
	}
}

func TestVFIOManager_ValidateDevices(t *testing.T) {
	logger := hclog.NewNullLogger()

	tests := []struct {
		name      string
		devices   []string
		allowlist []string
		setupFS   func(*MockFileSystem)
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "empty devices",
			devices:   []string{},
			allowlist: []string{"10de:*"},
			setupFS:   func(fs *MockFileSystem) {},
			wantErr:   false,
		},
		{
			name:      "valid nvidia device",
			devices:   []string{"0000:01:00.0"},
			allowlist: []string{"10de:*"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
			},
			wantErr: false,
		},
		{
			name:      "invalid pci address",
			devices:   []string{"invalid"},
			allowlist: []string{"10de:*"},
			setupFS:   func(fs *MockFileSystem) {},
			wantErr:   true,
			errMsg:    "invalid PCI address format",
		},
		{
			name:      "device not found",
			devices:   []string{"0000:01:00.0"},
			allowlist: []string{"10de:*"},
			setupFS:   func(fs *MockFileSystem) {},
			wantErr:   true,
			errMsg:    "PCI device not found",
		},
		{
			name:      "device not in allowlist",
			devices:   []string{"0000:01:00.0"},
			allowlist: []string{"8086:*"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
			},
			wantErr: true,
			errMsg:  "not in allowlist",
		},
		{
			name:      "multiple devices all valid",
			devices:   []string{"0000:01:00.0", "0000:02:00.0"},
			allowlist: []string{"10de:*", "8086:*"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
				fs.AddDevice("0000:02:00.0", "8086", "0d26", "ixgbe", "43")
			},
			wantErr: false,
		},
		{
			name:      "empty allowlist allows all",
			devices:   []string{"0000:01:00.0"},
			allowlist: []string{},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewMockFileSystem()
			tt.setupFS(fs)

			mgr := newVFIOManagerWithFS(logger, fs)
			err := mgr.ValidateDevices(tt.devices, tt.allowlist)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDevices() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !containsString(err.Error(), tt.errMsg) {
					t.Errorf("ValidateDevices() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestVFIOManager_CheckIOMMUGroups(t *testing.T) {
	logger := hclog.NewNullLogger()

	tests := []struct {
		name       string
		devices    []string
		setupFS    func(*MockFileSystem)
		wantGroups int
		wantErr    bool
		errMsg     string
	}{
		{
			name:    "single device single group",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
			},
			wantGroups: 1,
			wantErr:    false,
		},
		{
			name:    "two devices same group",
			devices: []string{"0000:01:00.0", "0000:01:00.1"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
				fs.AddDevice("0000:01:00.1", "10de", "2205", "nvidia", "42")
			},
			wantGroups: 1,
			wantErr:    false,
		},
		{
			name:    "two devices different groups",
			devices: []string{"0000:01:00.0", "0000:02:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
				fs.AddDevice("0000:02:00.0", "8086", "0d26", "ixgbe", "43")
			},
			wantGroups: 2,
			wantErr:    false,
		},
		{
			name:    "device without iommu group",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				// Add device but no IOMMU group
				fs.Files["/sys/bus/pci/devices/0000:01:00.0/vendor"] = "0x10de\n"
				fs.Files["/sys/bus/pci/devices/0000:01:00.0/device"] = "0x2204\n"
			},
			wantErr: true,
			errMsg:  "has no IOMMU group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewMockFileSystem()
			tt.setupFS(fs)

			mgr := newVFIOManagerWithFS(logger, fs)
			groups, err := mgr.CheckIOMMUGroups(tt.devices)

			if (err != nil) != tt.wantErr {
				t.Errorf("CheckIOMMUGroups() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !containsString(err.Error(), tt.errMsg) {
					t.Errorf("CheckIOMMUGroups() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if !tt.wantErr && len(groups) != tt.wantGroups {
				t.Errorf("CheckIOMMUGroups() returned %d groups, want %d", len(groups), tt.wantGroups)
			}
		})
	}
}

func TestVFIOManager_BindDevices(t *testing.T) {
	logger := hclog.NewNullLogger()

	tests := []struct {
		name          string
		devices       []string
		setupFS       func(*MockFileSystem)
		wantErr       bool
		checkWrites   bool
		expectedWrite string
	}{
		{
			name:    "bind single device",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
			},
			wantErr:       false,
			checkWrites:   true,
			expectedWrite: "/sys/bus/pci/drivers/vfio-pci/bind",
		},
		{
			name:    "device already bound to vfio-pci",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "vfio-pci", "42")
			},
			wantErr: false,
		},
		{
			name:    "bind multiple devices",
			devices: []string{"0000:01:00.0", "0000:02:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
				fs.AddDevice("0000:02:00.0", "8086", "0d26", "ixgbe", "43")
			},
			wantErr:     false,
			checkWrites: true,
		},
		{
			name:    "empty device list",
			devices: []string{},
			setupFS: func(fs *MockFileSystem) {},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewMockFileSystem()
			tt.setupFS(fs)

			mgr := newVFIOManagerWithFS(logger, fs)
			err := mgr.BindDevices(tt.devices)

			if (err != nil) != tt.wantErr {
				t.Errorf("BindDevices() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.checkWrites && tt.expectedWrite != "" {
				if _, written := fs.Writes[tt.expectedWrite]; !written {
					t.Errorf("BindDevices() did not write to %s", tt.expectedWrite)
				}
			}
		})
	}
}

func TestVFIOManager_GetVFIOGroupPaths(t *testing.T) {
	logger := hclog.NewNullLogger()

	tests := []struct {
		name      string
		devices   []string
		setupFS   func(*MockFileSystem)
		wantPaths int
		wantErr   bool
	}{
		{
			name:    "single device",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "vfio-pci", "42")
			},
			wantPaths: 1,
			wantErr:   false,
		},
		{
			name:    "two devices same group",
			devices: []string{"0000:01:00.0", "0000:01:00.1"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "vfio-pci", "42")
				fs.AddDevice("0000:01:00.1", "10de", "2205", "vfio-pci", "42")
			},
			wantPaths: 1, // Same group = same path
			wantErr:   false,
		},
		{
			name:    "two devices different groups",
			devices: []string{"0000:01:00.0", "0000:02:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "vfio-pci", "42")
				fs.AddDevice("0000:02:00.0", "8086", "0d26", "vfio-pci", "43")
			},
			wantPaths: 2,
			wantErr:   false,
		},
		{
			name:    "device without vfio group",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				// Device with IOMMU group but no /dev/vfio/ entry
				fs.Files["/sys/bus/pci/devices/0000:01:00.0/vendor"] = "0x10de\n"
				fs.Files["/sys/bus/pci/devices/0000:01:00.0/device"] = "0x2204\n"
				fs.Symlinks["/sys/bus/pci/devices/0000:01:00.0/iommu_group"] = "../../../../kernel/iommu_groups/42"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewMockFileSystem()
			tt.setupFS(fs)

			mgr := newVFIOManagerWithFS(logger, fs)
			paths, err := mgr.GetVFIOGroupPaths(tt.devices)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetVFIOGroupPaths() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(paths) != tt.wantPaths {
				t.Errorf("GetVFIOGroupPaths() returned %d paths, want %d", len(paths), tt.wantPaths)
			}
		})
	}
}

func TestVFIOManager_GetDeviceInfo(t *testing.T) {
	logger := hclog.NewNullLogger()

	tests := []struct {
		name    string
		device  string
		setupFS func(*MockFileSystem)
		wantErr bool
		check   func(*testing.T, *PCIDevice)
	}{
		{
			name:   "complete device info",
			device: "0000:01:00.0",
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
				fs.Files["/sys/bus/pci/devices/0000:01:00.0/numa_node"] = "0"
			},
			wantErr: false,
			check: func(t *testing.T, dev *PCIDevice) {
				if dev.Vendor != "10de" {
					t.Errorf("Vendor = %s, want 10de", dev.Vendor)
				}
				if dev.Device != "2204" {
					t.Errorf("Device = %s, want 2204", dev.Device)
				}
				if dev.Driver != "nvidia" {
					t.Errorf("Driver = %s, want nvidia", dev.Driver)
				}
				if dev.IOMMUGroup != "42" {
					t.Errorf("IOMMUGroup = %s, want 42", dev.IOMMUGroup)
				}
			},
		},
		{
			name:    "invalid pci address",
			device:  "invalid",
			setupFS: func(fs *MockFileSystem) {},
			wantErr: true,
		},
		{
			name:    "device not found",
			device:  "0000:01:00.0",
			setupFS: func(fs *MockFileSystem) {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewMockFileSystem()
			tt.setupFS(fs)

			mgr := newVFIOManagerWithFS(logger, fs)
			info, err := mgr.GetDeviceInfo(tt.device)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetDeviceInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.check != nil {
				tt.check(t, info)
			}
		})
	}
}

func TestVFIOManager_UnbindDevices(t *testing.T) {
	logger := hclog.NewNullLogger()

	tests := []struct {
		name        string
		devices     []string
		setupFS     func(*MockFileSystem)
		wantErr     bool
		checkWrites bool
	}{
		{
			name:    "unbind single device",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "vfio-pci", "42")
			},
			wantErr:     false,
			checkWrites: true,
		},
		{
			name:    "device not bound to vfio-pci",
			devices: []string{"0000:01:00.0"},
			setupFS: func(fs *MockFileSystem) {
				fs.AddDevice("0000:01:00.0", "10de", "2204", "nvidia", "42")
			},
			wantErr: false, // Should not error, just skip
		},
		{
			name:    "empty device list",
			devices: []string{},
			setupFS: func(fs *MockFileSystem) {},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewMockFileSystem()
			tt.setupFS(fs)

			mgr := newVFIOManagerWithFS(logger, fs)
			err := mgr.UnbindDevices(tt.devices)

			if (err != nil) != tt.wantErr {
				t.Errorf("UnbindDevices() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.checkWrites {
				unbindPath := "/sys/bus/pci/drivers/vfio-pci/unbind"
				if _, written := fs.Writes[unbindPath]; !written {
					t.Errorf("UnbindDevices() did not write to %s", unbindPath)
				}
			}
		})
	}
}

// Helper function for substring matching
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
