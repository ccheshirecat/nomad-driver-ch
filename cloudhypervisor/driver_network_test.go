package cloudhypervisor

import (
	"encoding/base64"
	"strings"
	"testing"

	domain "github.com/ccheshirecat/nomad-driver-ch/internal/shared"
	virtNet "github.com/ccheshirecat/nomad-driver-ch/virt/net"
	"github.com/hashicorp/go-hclog"
	"net/netip"
)

func TestDeriveNetworkSettingsDefaults(t *testing.T) {
	d := &Driver{
		logger:        hclog.NewNullLogger(),
		networkConfig: &domain.Network{Bridge: "br0", Gateway: "192.168.254.1"},
	}
	d.subnet = netip.MustParsePrefix("192.168.254.0/24")
	d.gatewayIP = netip.MustParseAddr("192.168.254.1")

	proc := &VMProcess{IP: "192.168.254.10"}
	settings, ok := d.deriveNetworkSettings(&domain.Config{}, proc)
	if !ok {
		t.Fatalf("expected settings for proc IP")
	}

	if settings.address != "192.168.254.10" {
		t.Fatalf("unexpected address %q", settings.address)
	}
	if settings.cidrBits != 24 {
		t.Fatalf("expected CIDR 24, got %d", settings.cidrBits)
	}
	if settings.gateway != "192.168.254.1" {
		t.Fatalf("expected gateway 192.168.254.1, got %q", settings.gateway)
	}
	if len(settings.nameservers) != 2 || settings.nameservers[0] != "8.8.8.8" {
		t.Fatalf("unexpected nameservers %#v", settings.nameservers)
	}
}

func TestDeriveNetworkSettingsOverrides(t *testing.T) {
	d := &Driver{
		logger:        hclog.NewNullLogger(),
		networkConfig: &domain.Network{Bridge: "br0", Gateway: "192.168.254.1"},
	}
	d.subnet = netip.MustParsePrefix("192.168.254.0/24")
	d.gatewayIP = netip.MustParseAddr("192.168.254.1")

	cfg := &domain.Config{
		NetworkInterfaces: virtNet.NetworkInterfacesConfig{
			&virtNet.NetworkInterfaceConfig{
				Bridge: &virtNet.NetworkInterfaceBridgeConfig{
					StaticIP: "10.0.0.50",
					Gateway:  "10.0.0.1",
					Netmask:  "25",
					DNS:      []string{"1.1.1.1", "9.9.9.9"},
				},
			},
		},
	}

	settings, ok := d.deriveNetworkSettings(cfg, &VMProcess{IP: "192.168.254.15"})
	if !ok {
		t.Fatalf("expected settings with overrides")
	}

	if settings.address != "10.0.0.50" {
		t.Fatalf("expected static IP override, got %q", settings.address)
	}
	if settings.gateway != "10.0.0.1" {
		t.Fatalf("expected gateway override, got %q", settings.gateway)
	}
	if settings.cidrBits != 25 {
		t.Fatalf("expected cidr 25, got %d", settings.cidrBits)
	}
	if len(settings.nameservers) != 2 || settings.nameservers[0] != "1.1.1.1" {
		t.Fatalf("unexpected nameservers %#v", settings.nameservers)
	}
}

func TestEnvFileFromMap(t *testing.T) {
	file, ok := envFileFromMap(map[string]string{
		"B": "value2",
		"A": "value1",
	})
	if !ok {
		t.Fatalf("expected env file to be generated")
	}
	if file.Path != envFilePath {
		t.Fatalf("unexpected file path %q", file.Path)
	}
	if file.Permissions != envFilePerms {
		t.Fatalf("unexpected permissions %q", file.Permissions)
	}

	decoded, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		t.Fatalf("failed decoding content: %v", err)
	}
	lines := strings.Split(string(decoded), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two export lines, got %v", lines)
	}
	if lines[0] != "export A=value1" || lines[1] != "export B=value2" {
		t.Fatalf("unexpected exports %v", lines)
	}
}

func TestBuildVMConfigAddsKernelIPParam(t *testing.T) {
	d := &Driver{
		logger:        hclog.NewNullLogger(),
		config:        &domain.CloudHypervisor{},
		networkConfig: &domain.Network{Bridge: "br0", Gateway: "192.168.254.1"},
	}
	d.subnet = netip.MustParsePrefix("192.168.254.0/24")
	d.gatewayIP = netip.MustParseAddr("192.168.254.1")

	config := &domain.Config{
		Name:      "alloc/test",
		BaseImage: "/tmp/disk.img",
		Kernel:    "/tmp/vmlinuz",
		Initramfs: "/tmp/initramfs",
		Memory:    512,
		CPUs:      1,
	}

	proc := &VMProcess{
		Name:    "alloc-test",
		IP:      "192.168.254.20",
		MAC:     "52:54:00:00:00:01",
		TapName: "tap1234",
		WorkDir: t.TempDir(),
	}

	vmConfig, err := d.buildVMConfig(config, proc)
	if err != nil {
		t.Fatalf("buildVMConfig failed: %v", err)
	}

	if !strings.Contains(vmConfig.Payload.Cmdline, "ip=192.168.254.20::192.168.254.1:255.255.255.0") {
		t.Fatalf("cmdline missing ip parameter: %q", vmConfig.Payload.Cmdline)
	}

	if len(vmConfig.Net) != 1 {
		t.Fatalf("expected single network config, got %d", len(vmConfig.Net))
	}
	if vmConfig.Net[0].IP != "192.168.254.20" {
		t.Fatalf("unexpected net IP %q", vmConfig.Net[0].IP)
	}
	if vmConfig.Net[0].Mask != "255.255.255.0" {
		t.Fatalf("unexpected net mask %q", vmConfig.Net[0].Mask)
	}
}
