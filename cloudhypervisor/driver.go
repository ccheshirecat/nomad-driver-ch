// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloudhypervisor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ccheshirecat/nomad-driver-ch/cloudinit"
	domain "github.com/ccheshirecat/nomad-driver-ch/internal/shared"
	"github.com/hashicorp/go-hclog"
)

const (
	// Cloud Hypervisor states mapped to our domain states
	CHStateRunning  = "running"
	CHStateShutdown = "shutdown"
	CHStateShutoff  = "shutoff"
	CHStateCrashed  = "crashed"
	CHStateUnknown  = "unknown"

	// Default timeouts and intervals
	defaultShutdownTimeout = 30 * time.Second
	defaultStartupTimeout  = 60 * time.Second

	envFilePath  = "/etc/profile.d/virt.sh"
	envFilePerms = "777"
)

// VMProcess represents a running VM process with its metadata
type VMProcess struct {
	Name          string
	Pid           int
	APISocket     string
	LogFile       string
	WorkDir       string
	TapName       string
	MAC           string
	IP            string
	VirtiofsdPIDs []int
	Config        *VMConfig
	StartedAt     time.Time
}

// Driver implements the Virtualizer interface for Cloud Hypervisor
type Driver struct {
	logger        hclog.Logger
	config        *domain.CloudHypervisor
	networkConfig *domain.Network
	vfioConfig    *domain.VFIO
	dataDir       string

	// Registry of running VMs
	mu        sync.RWMutex
	processes map[string]*VMProcess

	// HTTP client for Unix socket communication
	httpClient *http.Client

	// Cloud-init controller
	ci CloudInit

	// IP allocation state
	allocatedIPs map[string]bool // IP -> allocated

	// Parsed network configuration for quick reuse
	ipPoolStart netip.Addr
	ipPoolEnd   netip.Addr
	subnet      netip.Prefix
	gatewayIP   netip.Addr

	// For testing - skip binary validation
	skipBinaryValidation bool
}

// CloudInit interface for generating cloud-init ISOs
type CloudInit interface {
	Apply(ci *cloudinit.Config, path string) error
}

func prefixBitsToNetmask(bits int) string {
	if bits < 0 || bits > 32 {
		return ""
	}
	var mask uint32
	if bits == 0 {
		mask = 0
	} else {
		mask = ^uint32(0) << (32 - bits)
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(mask>>24),
		byte(mask>>16),
		byte(mask>>8),
		byte(mask))
}

type networkSettings struct {
	address       string
	gateway       string
	cidrBits      int
	nameservers   []string
	interfaceName string
}

func (n networkSettings) cidrString() string {
	return strconv.Itoa(n.cidrBits)
}

func (n networkSettings) netmaskDotted() string {
	mask := prefixBitsToNetmask(n.cidrBits)
	if mask == "" {
		return "255.255.255.0"
	}
	return mask
}

func (d *Driver) deriveNetworkSettings(config *domain.Config, proc *VMProcess) (networkSettings, bool) {
	settings := networkSettings{
		interfaceName: "eth0",
		cidrBits:      -1,
		nameservers:   []string{"8.8.8.8", "8.8.4.4"},
	}

	if proc != nil && proc.IP != "" {
		settings.address = proc.IP
	}

	if d.subnet.IsValid() {
		settings.cidrBits = d.subnet.Bits()
	}

	if d.gatewayIP.IsValid() {
		settings.gateway = d.gatewayIP.String()
	} else if d.networkConfig != nil && d.networkConfig.Gateway != "" {
		settings.gateway = d.networkConfig.Gateway
	}

	if config != nil && len(config.NetworkInterfaces) > 0 {
		if bridge := config.NetworkInterfaces[0].Bridge; bridge != nil {
			if bridge.StaticIP != "" {
				settings.address = bridge.StaticIP
			}
			if bridge.Gateway != "" {
				settings.gateway = bridge.Gateway
			}
			if bridge.Netmask != "" {
				if bits, err := strconv.Atoi(bridge.Netmask); err == nil && bits >= 0 && bits <= 32 {
					settings.cidrBits = bits
				} else {
					d.logger.Warn("invalid bridge netmask, falling back to defaults", "value", bridge.Netmask, "vm", config.Name)
				}
			}
			if len(bridge.DNS) > 0 {
				settings.nameservers = append([]string{}, bridge.DNS...)
			}
		}
	}

	if settings.cidrBits < 0 {
		settings.cidrBits = 24
	}

	if settings.address == "" {
		return settings, false
	}

	return settings, true
}

func envFileFromMap(env map[string]string) (domain.File, bool) {
	if len(env) == 0 {
		return domain.File{}, false
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("export %s=%s", k, env[k]))
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
	return domain.File{
		Path:        envFilePath,
		Permissions: envFilePerms,
		Encoding:    "b64",
		Content:     encoded,
	}, true
}

func upsertFile(files []domain.File, file domain.File) []domain.File {
	for i := range files {
		if files[i].Path == file.Path {
			files[i] = file
			return files
		}
	}
	return append(files, file)
}

// VMConfig represents the JSON structure for CH vm.create API
type VMConfig struct {
	CPUs     CPUConfig       `json:"cpus"`
	Memory   MemoryConfig    `json:"memory"`
	Payload  *PayloadConfig  `json:"payload,omitempty"`
	Disks    []DiskConfig    `json:"disks,omitempty"`
	Net      []NetConfig     `json:"net,omitempty"`
	RNG      *RNGConfig      `json:"rng,omitempty"`
	Vsock    *VsockConfig    `json:"vsock,omitempty"`
	FS       []FSConfig      `json:"fs,omitempty"`
	Platform *PlatformConfig `json:"platform,omitempty"`
	Devices  []DeviceConfig  `json:"devices,omitempty"`
	Console  ConsoleConfig   `json:"console"`
	Serial   SerialConfig    `json:"serial"`
}

type CPUConfig struct {
	BootVCPUs uint     `json:"boot_vcpus"`
	MaxVCPUs  uint     `json:"max_vcpus"`
	Features  []string `json:"features,omitempty"`
}

type MemoryConfig struct {
	Size          int64  `json:"size"`
	Shared        bool   `json:"shared,omitempty"`
	Hugepages     bool   `json:"hugepages,omitempty"`
	HotplugMethod string `json:"hotplug_method,omitempty"`
	HotplugSize   int64  `json:"hotplug_size,omitempty"`
}

type PayloadConfig struct {
	Kernel    string `json:"kernel"`
	Cmdline   string `json:"cmdline"`
	Initramfs string `json:"initramfs,omitempty"`
}

type DiskConfig struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly,omitempty"`
	Serial   string `json:"serial,omitempty"`
}

type NetConfig struct {
	Tap  string `json:"tap"`
	MAC  string `json:"mac"`
	IP   string `json:"ip,omitempty"`
	Mask string `json:"mask,omitempty"`
}

type RNGConfig struct {
	Src string `json:"src"`
}

type VsockConfig struct {
	CID    uint   `json:"cid"`
	Socket string `json:"socket"`
}

type FSConfig struct {
	Tag       string `json:"tag"`
	Socket    string `json:"socket"`
	NumQueues uint   `json:"num_queues,omitempty"`
	QueueSize uint   `json:"queue_size,omitempty"`
}

type PlatformConfig struct {
	NumPCISegments    uint   `json:"num_pci_segments,omitempty"`
	IOMMUSegments     []uint `json:"iommu_segments,omitempty"`
	IOMMUAddressWidth uint   `json:"iommu_address_width,omitempty"`
}

type DeviceConfig struct {
	Path       string `json:"path"`
	ID         string `json:"id,omitempty"`
	IOMMU      bool   `json:"iommu,omitempty"`
	PCISegment uint   `json:"pci_segment,omitempty"`
}

// VFIODeviceConfig represents a VFIO device configuration
type VFIODeviceConfig struct {
	Path       string `json:"path"`
	ID         string `json:"id,omitempty"`
	IOMMU      bool   `json:"iommu,omitempty"`
	PCISegment uint   `json:"pci_segment,omitempty"`
}

type ConsoleConfig struct {
	Mode string `json:"mode"`
}

type SerialConfig struct {
	Mode string `json:"mode"`
	File string `json:"file,omitempty"`
}

// VMInfo represents the response from CH vm.info API
type VMInfo struct {
	State  string `json:"state"`
	Memory struct {
		ActualSize uint64 `json:"actual_size"`
		LastUpdate uint64 `json:"last_update_ts"`
	} `json:"memory"`
	Balloons []interface{} `json:"balloons"`
	Block    []interface{} `json:"block"`
	Net      []interface{} `json:"net"`
}

// New creates a new Cloud Hypervisor driver
func New(ctx context.Context, logger hclog.Logger, config *domain.CloudHypervisor, netConfig *domain.Network, vfioConfig *domain.VFIO, dataDir string) *Driver {
	return NewWithSkipValidation(ctx, logger, config, netConfig, vfioConfig, dataDir, false)
}

// NewWithSkipValidation creates a new Cloud Hypervisor driver with optional binary validation skip
func NewWithSkipValidation(ctx context.Context, logger hclog.Logger, config *domain.CloudHypervisor, netConfig *domain.Network, vfioConfig *domain.VFIO, dataDir string, skipValidation bool) *Driver {
	d := &Driver{
		logger:               logger.Named("cloud-hypervisor"),
		config:               config,
		networkConfig:        netConfig,
		vfioConfig:           vfioConfig,
		dataDir:              dataDir,
		processes:            make(map[string]*VMProcess),
		allocatedIPs:         make(map[string]bool),
		skipBinaryValidation: skipValidation,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					// This will be overridden per request with the actual socket path
					return nil, fmt.Errorf("socket path not set")
				},
			},
			Timeout: 30 * time.Second,
		},
	}

	d.initializeNetworkConfig()

	go d.monitorCtx(ctx)
	return d
}

// initializeNetworkConfig pre-parses the network configuration so we can allocate
// deterministic IP addresses and populate cloud-init without relying on
// hard-coded defaults.
func (d *Driver) initializeNetworkConfig() {
	if d.networkConfig == nil {
		return
	}

	if cidr := d.networkConfig.SubnetCIDR; cidr != "" {
		if prefix, err := netip.ParsePrefix(cidr); err == nil {
			d.subnet = prefix.Masked()
		} else {
			d.logger.Warn("invalid subnet CIDR, network defaults will be skipped", "cidr", cidr, "error", err)
		}
	}

	if start := d.networkConfig.IPPoolStart; start != "" {
		if addr, err := netip.ParseAddr(start); err == nil {
			d.ipPoolStart = addr
		} else {
			d.logger.Warn("invalid IP pool start", "ip", start, "error", err)
		}
	}

	if end := d.networkConfig.IPPoolEnd; end != "" {
		if addr, err := netip.ParseAddr(end); err == nil {
			d.ipPoolEnd = addr
		} else {
			d.logger.Warn("invalid IP pool end", "ip", end, "error", err)
		}
	}

	if gw := d.networkConfig.Gateway; gw != "" {
		if addr, err := netip.ParseAddr(gw); err == nil {
			d.gatewayIP = addr
		} else {
			d.logger.Warn("invalid gateway", "gateway", gw, "error", err)
		}
	}

	if d.subnet.IsValid() {
		// Ensure the gateway defaults to the first usable address if not explicitly set.
		if !d.gatewayIP.IsValid() {
			networkAddr := d.subnet.Addr()
			next := networkAddr.Next()
			if next.IsValid() && d.subnet.Contains(next) {
				d.gatewayIP = next
			}
		}

		// Validate that pool bounds fall within the subnet; warn otherwise.
		if d.ipPoolStart.IsValid() && !d.subnet.Contains(d.ipPoolStart) {
			d.logger.Warn("IP pool start is outside configured subnet", "ip", d.ipPoolStart.String(), "subnet", d.subnet.String())
			d.ipPoolStart = netip.Addr{}
		}
		if d.ipPoolEnd.IsValid() && !d.subnet.Contains(d.ipPoolEnd) {
			d.logger.Warn("IP pool end is outside configured subnet", "ip", d.ipPoolEnd.String(), "subnet", d.subnet.String())
			d.ipPoolEnd = netip.Addr{}
		}
	}

}

func (d *Driver) ensureBridgeConfigured() error {
	ipPath, err := findIPCommand()
	if err != nil {
		// When running in test environments we may not have ip; skip silently.
		if d.skipBinaryValidation {
			return nil
		}
		return err
	}

	bridge := d.networkConfig.Bridge
	if bridge == "" {
		return fmt.Errorf("bridge name not provided")
	}

	if err := exec.Command(ipPath, "link", "show", bridge).Run(); err != nil {
		d.logger.Info("bridge not found, creating", "bridge", bridge)
		if output, createErr := exec.Command(ipPath, "link", "add", "name", bridge, "type", "bridge").CombinedOutput(); createErr != nil {
			if !strings.Contains(strings.ToLower(string(output)), "file exists") {
				return fmt.Errorf("unable to create bridge %s: %w (output: %s)", bridge, createErr, strings.TrimSpace(string(output)))
			}
		}
	}

	addrOutput, addrErr := exec.Command(ipPath, "addr", "show", bridge).CombinedOutput()
	if addrErr != nil {
		return fmt.Errorf("unable to read bridge addresses: %w (output: %s)", addrErr, strings.TrimSpace(string(addrOutput)))
	}

	if !strings.Contains(string(addrOutput), d.gatewayIP.String()) {
		cidr := fmt.Sprintf("%s/%d", d.gatewayIP.String(), d.subnet.Bits())
		if output, addErr := exec.Command(ipPath, "addr", "add", cidr, "dev", bridge).CombinedOutput(); addErr != nil {
			if !strings.Contains(strings.ToLower(string(output)), "file exists") {
				return fmt.Errorf("unable to assign %s to %s: %w (output: %s)", cidr, bridge, addErr, strings.TrimSpace(string(output)))
			}
		}
	}

	if output, upErr := exec.Command(ipPath, "link", "set", bridge, "up").CombinedOutput(); upErr != nil {
		return fmt.Errorf("unable to bring bridge %s up: %w (output: %s)", bridge, upErr, strings.TrimSpace(string(output)))
	}

	return nil
}

func (d *Driver) validateNetworkConfig() error {
	if d.networkConfig == nil {
		return fmt.Errorf("network configuration is not set")
	}

	if d.networkConfig.Bridge == "" {
		return fmt.Errorf("network bridge is not configured")
	}

	if !d.subnet.IsValid() {
		return fmt.Errorf("network subnet_cidr must be configured (e.g. 192.168.254.0/24)")
	}

	if !d.gatewayIP.IsValid() || !d.subnet.Contains(d.gatewayIP) {
		return fmt.Errorf("gateway %s must be a valid IPv4 address within %s", d.gatewayIP.String(), d.subnet.String())
	}

	if !d.ipPoolStart.IsValid() || !d.ipPoolEnd.IsValid() {
		return fmt.Errorf("ip_pool_start and ip_pool_end must be configured within %s", d.subnet.String())
	}

	if !d.subnet.Contains(d.ipPoolStart) || !d.subnet.Contains(d.ipPoolEnd) {
		return fmt.Errorf("IP pool %s-%s must fall within subnet %s", d.ipPoolStart.String(), d.ipPoolEnd.String(), d.subnet.String())
	}

	if d.ipPoolEnd.Compare(d.ipPoolStart) < 0 {
		return fmt.Errorf("ip_pool_end %s precedes ip_pool_start %s", d.ipPoolEnd.String(), d.ipPoolStart.String())
	}

	if d.gatewayIP == d.ipPoolStart {
		return fmt.Errorf("gateway %s conflicts with ip_pool_start", d.gatewayIP.String())
	}

	return nil
}

// monitorCtx handles context cancellation cleanup
func (d *Driver) monitorCtx(ctx context.Context) {
	<-ctx.Done()
	d.logger.Info("shutting down cloud hypervisor driver")

	d.mu.Lock()
	defer d.mu.Unlock()

	// Cleanup all running processes
	for name, proc := range d.processes {
		d.logger.Warn("forcefully stopping VM on shutdown", "vm", name)
		d.cleanupProcess(nil, proc)
	}
}

// Start validates the Cloud Hypervisor installation and initializes the driver
func (d *Driver) Start(dataDir string) error {
	if dataDir != "" {
		d.dataDir = dataDir
	}

	// Validate CH binaries exist and are executable
	if err := d.validateBinaries(); err != nil {
		return fmt.Errorf("cloud hypervisor binary validation failed: %w", err)
	}

	// Initialize cloud-init controller
	ci, err := cloudinit.NewController(d.logger.Named("cloud-init"))
	if err != nil {
		return fmt.Errorf("failed to create cloud-init controller: %w", err)
	}
	d.ci = ci

	if err := d.validateNetworkConfig(); err != nil {
		return fmt.Errorf("invalid network configuration: %w", err)
	}

	if err := d.ensureBridgeConfigured(); err != nil {
		return fmt.Errorf("failed to configure bridge %s: %w", d.networkConfig.Bridge, err)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(d.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	d.logger.Info("cloud hypervisor driver started successfully",
		"data_dir", d.dataDir,
		"ch_binary", d.config.Bin)

	return nil
}

// validateBinaries checks that required binaries are available
func (d *Driver) validateBinaries() error {
	// Skip validation for testing
	if d.skipBinaryValidation {
		return nil
	}

	binaries := map[string]string{
		"cloud-hypervisor": d.config.Bin,
		"ch-remote":        d.config.RemoteBin,
	}

	// virtiofsd is optional
	if d.config.VirtiofsdBin != "" {
		binaries["virtiofsd"] = d.config.VirtiofsdBin
	}

	for name, path := range binaries {
		if path == "" {
			continue // Optional binary
		}

		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s binary not found at %s: %w", name, path, err)
		}

		// Check if executable
		if err := exec.Command(path, "--version").Run(); err != nil {
			d.logger.Warn("binary version check failed", "binary", name, "path", path, "error", err)
		}
	}

	return nil
}

// CreateDomain creates and starts a new VM
func (d *Driver) CreateDomain(config *domain.Config, env map[string]string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if VM already exists
	if _, exists := d.processes[config.Name]; exists {
		return fmt.Errorf("VM %s already exists", config.Name)
	}

	d.logger.Info("creating VM", "name", config.Name)

	// Create working directory for this VM
	workDir := filepath.Join(d.dataDir, config.Name)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Build VM process info
	proc := &VMProcess{
		Name:      config.Name,
		WorkDir:   workDir,
		APISocket: filepath.Join(workDir, "api.sock"),
		LogFile:   filepath.Join(workDir, "vmm.log"),
		StartedAt: time.Now(),
	}

	// Allocate IP address - use task-specific static IP if provided, otherwise allocate from pool
	var ip string
	if len(config.NetworkInterfaces) > 0 && config.NetworkInterfaces[0].Bridge != nil && config.NetworkInterfaces[0].Bridge.StaticIP != "" {
		// Use task-specified static IP
		ip = config.NetworkInterfaces[0].Bridge.StaticIP
		if d.subnet.IsValid() {
			if addr, err := netip.ParseAddr(ip); err == nil && !d.subnet.Contains(addr) {
				return fmt.Errorf("static IP %s is outside configured subnet %s", ip, d.subnet.String())
			}
		}

		if d.allocatedIPs[ip] {
			return fmt.Errorf("static IP %s is already allocated", ip)
		}

		d.allocatedIPs[ip] = true
		d.logger.Info("using task-specified static IP", "ip", ip, "vm", config.Name)
	} else {
		// Allocate IP from pool
		var err error
		ip, err = d.allocateIP()
		if err != nil {
			return fmt.Errorf("failed to allocate IP: %w", err)
		}
		d.logger.Debug("allocated IP from pool", "ip", ip, "vm", config.Name)
	}
	proc.IP = ip

	if env == nil {
		env = make(map[string]string)
	}

	if settings, ok := d.deriveNetworkSettings(config, proc); ok {
		env["VM_IP"] = settings.address
		env["VM_CIDR"] = settings.cidrString()
		if settings.gateway != "" {
			env["VM_GATEWAY"] = settings.gateway
		} else {
			delete(env, "VM_GATEWAY")
		}
		if len(settings.nameservers) > 0 {
			env["VM_DNS"] = strings.Join(settings.nameservers, " ")
		} else {
			delete(env, "VM_DNS")
		}
	}

	if file, ok := envFileFromMap(env); ok {
		config.Files = upsertFile(config.Files, file)
	}

	// Generate MAC address deterministically
	proc.MAC = d.generateMAC(config.Name)

	// Generate short TAP name to fit Linux's 15-char limit (IFNAMSIZ)
	// Use prefix + hash of name + current nanosecond time for uniqueness
	uniqueStr := fmt.Sprintf("%s-%d", config.Name, time.Now().UnixNano())
	nameHash := fmt.Sprintf("%x", sha256.Sum256([]byte(uniqueStr)))[:8]
	proc.TapName = d.networkConfig.TAPPrefix + nameHash

	// Create cloud-init ISO
	if err := d.createCloudInit(config, proc, workDir); err != nil {
		d.deallocateIP(ip)
		return fmt.Errorf("failed to create cloud-init: %w", err)
	}

	// Setup networking (create TAP interface)
	if err := d.setupNetworking(config, proc); err != nil {
		d.deallocateIP(ip)
		return fmt.Errorf("failed to setup networking: %w", err)
	}

	// Start virtiofsd processes for mounts
	if err := d.startVirtiofsd(config, proc); err != nil {
		d.deallocateIP(ip)
		d.cleanupNetworking(config, proc)
		d.cleanupProcess(config, proc)
		return fmt.Errorf("failed to start virtiofsd: %w", err)
	}

	// Build CH VM configuration
	vmConfig, err := d.buildVMConfig(config, proc)
	if err != nil {
		d.deallocateIP(ip)
		d.cleanupNetworking(config, proc)
		d.stopVirtiofsd(proc)
		d.cleanupProcess(config, proc)
		return fmt.Errorf("failed to build VM config: %w", err)
	}

	// Add VFIO devices if configured
	if err := d.addVFIODevices(config, vmConfig); err != nil {
		d.deallocateIP(ip)
		d.cleanupNetworking(config, proc)
		d.stopVirtiofsd(proc)
		return fmt.Errorf("failed to add VFIO devices: %w", err)
	}

	proc.Config = vmConfig

	// Start Cloud Hypervisor process
	if err := d.startCHProcess(proc); err != nil {
		d.deallocateIP(ip)
		d.cleanupNetworking(config, proc)
		d.stopVirtiofsd(proc)
		d.cleanupProcess(config, proc)
		return fmt.Errorf("failed to start CH process: %w", err)
	}

	// Create and boot VM via REST API
	if err := d.createAndBootVM(proc); err != nil {
		d.cleanupProcess(config, proc)
		d.deallocateIP(ip)
		return fmt.Errorf("failed to create/boot VM: %w", err)
	}

	// Register the process
	d.processes[config.Name] = proc

	d.logger.Info("VM created successfully",
		"name", config.Name,
		"ip", proc.IP,
		"mac", proc.MAC,
		"tap", proc.TapName)

	return nil
}

// StopDomain gracefully stops a VM
func (d *Driver) StopDomain(name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	proc, exists := d.processes[name]
	if !exists {
		return fmt.Errorf("VM %s not found", name)
	}

	d.logger.Info("stopping VM", "name", name)

	// Try graceful shutdown via REST API first
	if err := d.shutdownVM(proc); err != nil {
		d.logger.Warn("graceful shutdown failed, forcing stop", "vm", name, "error", err)
		// Force kill the process
		if proc.Pid > 0 {
			if process, err := os.FindProcess(proc.Pid); err == nil {
				process.Kill()
			}
		}
	}

	return nil
}

// DestroyDomain stops and removes a VM completely
func (d *Driver) DestroyDomain(name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	proc, exists := d.processes[name]
	if !exists {
		return fmt.Errorf("VM %s not found", name)
	}

	d.logger.Info("destroying VM", "name", name)

	// Stop the VM first
	d.shutdownVM(proc)

	// Cleanup everything
	d.cleanupProcess(nil, proc)

	// Deallocate IP
	d.deallocateIP(proc.IP)

	// Remove from registry
	delete(d.processes, name)

	d.logger.Info("VM destroyed", "name", name)
	return nil
}

// GetInfo returns information about the Cloud Hypervisor host
func (d *Driver) GetInfo() (domain.VirtualizerInfo, error) {
	info := domain.VirtualizerInfo{
		Model: "cloud-hypervisor",
	}

	// Get CH version
	if version, err := d.getCHVersion(); err == nil {
		if v, err := strconv.ParseUint(version, 10, 32); err == nil {
			info.EmulatorVersion = uint32(v)
		}
	}

	// Count running VMs
	d.mu.RLock()
	info.RunningDomains = uint(len(d.processes))
	d.mu.RUnlock()

	// TODO: Get actual host memory/CPU info if needed
	// For now, leave as defaults (0 values)

	return info, nil
}

// getCHVersion extracts version from cloud-hypervisor --version
func (d *Driver) getCHVersion() (string, error) {
	cmd := exec.Command(d.config.Bin, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Parse "cloud-hypervisor v48.0.0" -> "48"
	versionStr := strings.TrimSpace(string(output))
	parts := strings.Fields(versionStr)
	if len(parts) >= 2 {
		version := strings.TrimPrefix(parts[1], "v")
		// Extract major version number
		if dotIndex := strings.Index(version, "."); dotIndex > 0 {
			return version[:dotIndex], nil
		}
		return version, nil
	}

	return "0", nil
}

// GetNetworkInterfaces returns network interface information for a VM
func (d *Driver) GetNetworkInterfaces(name string) ([]domain.NetworkInterface, error) {
	d.mu.RLock()
	proc, exists := d.processes[name]
	d.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("VM %s not found", name)
	}

	// Return interface info from our stored configuration
	interfaces := []domain.NetworkInterface{
		{
			NetworkName: d.networkConfig.Bridge,
			DeviceName:  proc.TapName,
			MAC:         proc.MAC,
			Model:       "virtio",
			Driver:      "virtio-net",
		},
	}

	// Parse IP address if available
	if proc.IP != "" {
		if addr, err := netip.ParseAddr(proc.IP); err == nil {
			interfaces[0].Addrs = []netip.Addr{addr}
		}
	}

	return interfaces, nil
}

// GetDomain returns information about a specific VM
func (d *Driver) GetDomain(name string) (*domain.Info, error) {
	d.mu.RLock()
	proc, exists := d.processes[name]
	d.mu.RUnlock()

	if !exists {
		return nil, nil // VM not found
	}

	// Query VM info via REST API
	info, err := d.getVMInfo(proc)
	if err != nil {
		// If REST API fails, check if process is still running
		if proc.Pid > 0 {
			if process, err := os.FindProcess(proc.Pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					// Process exists, assume running
					return &domain.Info{
						State: CHStateRunning,
					}, nil
				}
			}
		}
		// Process not found, VM is stopped
		return &domain.Info{
			State: CHStateShutoff,
		}, nil
	}

	// Map CH state to domain state
	domainState := mapCHState(info.State)

	return &domain.Info{
		State:     domainState,
		Memory:    info.Memory.ActualSize,
		MaxMemory: info.Memory.ActualSize, // CH doesn't distinguish
		CPUTime:   0,                      // TODO: extract if available
	}, nil
}

// Helper functions below...

func (d *Driver) allocateIP() (string, error) {
	if !d.ipPoolStart.IsValid() || !d.ipPoolEnd.IsValid() {
		return "", fmt.Errorf("IP pool is not configured; set network.ip_pool_start and network.ip_pool_end")
	}

	for ip := d.ipPoolStart; ; {
		if d.subnet.IsValid() && !d.subnet.Contains(ip) {
			return "", fmt.Errorf("allocated IP %s is outside configured subnet %s", ip.String(), d.subnet.String())
		}

		if d.gatewayIP.IsValid() && ip == d.gatewayIP {
			if ip == d.ipPoolEnd {
				break
			}
			next := ip.Next()
			if !next.IsValid() {
				break
			}
			ip = next
			continue
		}

		ipStr := ip.String()
		if !d.allocatedIPs[ipStr] {
			d.allocatedIPs[ipStr] = true
			return ipStr, nil
		}

		if ip == d.ipPoolEnd {
			break
		}

		next := ip.Next()
		if !next.IsValid() {
			break
		}
		ip = next
	}

	return "", fmt.Errorf("no available IPs in pool %s-%s", d.ipPoolStart.String(), d.ipPoolEnd.String())
}

func (d *Driver) deallocateIP(ip string) {
	delete(d.allocatedIPs, ip)
}

func (d *Driver) generateMAC(vmName string) string {
	// Generate deterministic MAC address based on VM name
	// Use a simple hash-based approach
	hash := 0
	for _, c := range vmName {
		hash = hash*31 + int(c)
	}

	// Generate MAC with 52:54:00 prefix (QEMU/KVM range)
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x",
		byte(hash>>16), byte(hash>>8), byte(hash))
}

func mapCHState(chState string) string {
	switch strings.ToLower(chState) {
	case "running":
		return CHStateRunning
	case "shutdown":
		return CHStateShutdown
	case "shutoff":
		return CHStateShutoff
	case "crashed":
		return CHStateCrashed
	default:
		return CHStateUnknown
	}
}

func parseIPAddr(ipStr string) (net.IP, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ipStr)
	}
	return ip, nil
}

func maskStringFromPrefix(prefix netip.Prefix) string {
	if !prefix.IsValid() {
		return ""
	}

	addr := prefix.Addr()
	if !addr.Is4() {
		return ""
	}

	mask := net.CIDRMask(prefix.Bits(), 32)
	if len(mask) != net.IPv4len {
		return ""
	}

	return fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
}

// addVFIODevices adds VFIO devices to the VM configuration
func (d *Driver) addVFIODevices(config *domain.Config, vmConfig *VMConfig) error {
	// Extract VFIO devices from domain config (populated from task config)
	vfioDevices := config.VFIODevices

	if len(vfioDevices) == 0 {
		// No VFIO devices to configure
		return nil
	}

	d.logger.Info("configuring VFIO device passthrough", "vm", config.Name, "devices", vfioDevices)

	// Create VFIO manager
	vfioMgr := NewVFIOManager(d.logger)

	// Get allowlist from driver VFIO config
	var allowlist []string
	if d.vfioConfig != nil && len(d.vfioConfig.Allowlist) > 0 {
		allowlist = d.vfioConfig.Allowlist
		d.logger.Debug("using VFIO allowlist", "allowlist", allowlist)
	} else {
		d.logger.Warn("no VFIO allowlist configured - all devices will be allowed")
	}

	// Step 1: Validate devices against allowlist
	if err := vfioMgr.ValidateDevices(vfioDevices, allowlist); err != nil {
		return fmt.Errorf("VFIO device validation failed: %w", err)
	}

	// Step 2: Check IOMMU groups
	groups, err := vfioMgr.CheckIOMMUGroups(vfioDevices)
	if err != nil {
		return fmt.Errorf("failed to check IOMMU groups: %w", err)
	}

	d.logger.Info("IOMMU groups checked", "vm", config.Name, "groups", len(groups))
	for _, group := range groups {
		d.logger.Debug("IOMMU group", "id", group.ID, "devices", group.Devices)
	}

	// Step 3: Bind devices to vfio-pci driver
	if err := vfioMgr.BindDevices(vfioDevices); err != nil {
		return fmt.Errorf("failed to bind VFIO devices: %w", err)
	}

	// Step 4: Get VFIO group device paths
	groupPaths, err := vfioMgr.GetVFIOGroupPaths(vfioDevices)
	if err != nil {
		// Cleanup: unbind devices on failure
		vfioMgr.UnbindDevices(vfioDevices)
		return fmt.Errorf("failed to get VFIO group paths: %w", err)
	}

	// Step 5: Configure platform for IOMMU if needed
	if vmConfig.Platform == nil {
		vmConfig.Platform = &PlatformConfig{}
	}

	// Set platform configuration for VFIO from driver config or defaults
	if vmConfig.Platform.NumPCISegments == 0 {
		if d.vfioConfig != nil && d.vfioConfig.PCISegments > 0 {
			vmConfig.Platform.NumPCISegments = d.vfioConfig.PCISegments
		} else {
			vmConfig.Platform.NumPCISegments = 1 // Default
		}
	}
	if vmConfig.Platform.IOMMUAddressWidth == 0 {
		if d.vfioConfig != nil && d.vfioConfig.IOMMUAddressWidth > 0 {
			vmConfig.Platform.IOMMUAddressWidth = d.vfioConfig.IOMMUAddressWidth
		} else {
			vmConfig.Platform.IOMMUAddressWidth = 48 // Default
		}
	}
	if len(vmConfig.Platform.IOMMUSegments) == 0 {
		vmConfig.Platform.IOMMUSegments = []uint{0}
	}

	// Step 6: Add VFIO devices to VM configuration
	if vmConfig.Devices == nil {
		vmConfig.Devices = []DeviceConfig{}
	}

	for i, groupPath := range groupPaths {
		deviceConfig := DeviceConfig{
			Path:       groupPath,
			ID:         fmt.Sprintf("vfio-%d", i),
			IOMMU:      true,
			PCISegment: 0,
		}
		vmConfig.Devices = append(vmConfig.Devices, deviceConfig)
		d.logger.Debug("added VFIO device to VM config", "path", groupPath, "id", deviceConfig.ID)
	}

	d.logger.Info("VFIO device passthrough configured successfully",
		"vm", config.Name,
		"devices", len(vfioDevices),
		"groups", len(groupPaths))

	return nil
}

// Additional methods will be implemented in separate files to keep this manageable
// These include:
// - createCloudInit()
// - setupNetworking()
// - startVirtiofsd()
// - buildVMConfig()
// - startCHProcess()
// - createAndBootVM()
// - shutdownVM()
// - getVMInfo()
// - cleanupProcess()
// - etc.
