// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build linux

package chnet

import (
	"bufio"
	"errors"
	"fmt"
	stdnet "net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	domain "github.com/ccheshirecat/nomad-driver-ch/internal/shared"
	"github.com/ccheshirecat/nomad-driver-ch/virt/net"
	"github.com/coreos/go-iptables/iptables"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/structs"
)

const (
	// preroutingIPTablesChainName is the IPTables chain name used by the
	// driver for prerouting rules. This is currently used for entries within
	// the nat table.
	preroutingIPTablesChainName = "NOMAD_CH_PRT"

	// forwardIPTablesChainName is the IPTables chain name used by the driver
	// for forwarding rules. This is currently used for entries within the
	// filter table.
	forwardIPTablesChainName = "NOMAD_CH_FW"

	// iptablesNATTableName is the name of the nat table within iptables.
	iptablesNATTableName = "nat"

	// iptablesFilterTableName is the name of the filter table within iptables.
	iptablesFilterTableName = "filter"

	// NetworkStateActive is string representation to declare a network is in
	// active state.
	NetworkStateActive = "active"

	// NetworkStateInactive is string representation to declare a network is in
	// inactive state.
	NetworkStateInactive = "inactive"

	// Default dnsmasq lease file path
	defaultDnsmasqLeaseFile = "/var/lib/misc/dnsmasq.leases"
)

// Controller implements the Net interface for Cloud Hypervisor networking
// without depending on libvirt. It manages static IP allocation and iptables
// rules for port forwarding.
type Controller struct {
	logger        hclog.Logger
	networkConfig *domain.Network

	// interfaceByIPGetter is the function that queries the host using the
	// passed IP address and identifies the interface it is assigned to. It is
	// a field within the controller to aid testing.
	interfaceByIPGetter
}

// interfaceByIPGetter is the function signature used to identify the host's
// interface using a passed IP address. This is primarily used for testing,
// where we don't know the host, and we want to ensure stability and
// consistency when this is called.
type interfaceByIPGetter func(ip stdnet.IP) (string, error)

// NewController returns a Controller which implements the net.Net interface
// for Cloud Hypervisor networking.
func NewController(logger hclog.Logger, networkConfig *domain.Network) *Controller {
	return &Controller{
		logger:              logger.Named("chnet"),
		networkConfig:       networkConfig,
		interfaceByIPGetter: getInterfaceByIP,
	}
}

// Fingerprint interrogates the host system and populates the attribute
// mapping with relevant network information. For CH, we check the configured
// bridge interface status.
func (c *Controller) Fingerprint(attr map[string]*structs.Attribute) {
	bridgeName := c.networkConfig.Bridge

	// Check if bridge exists and is active
	state := c.getBridgeState(bridgeName)

	// Populate the attributes mapping with our bridge state
	bridgeStateKey := net.FingerprintAttributeKeyPrefix + bridgeName + ".state"
	attr[bridgeStateKey] = structs.NewStringAttribute(state)

	// Add bridge name attribute
	bridgeNameKey := net.FingerprintAttributeKeyPrefix + bridgeName + ".bridge_name"
	attr[bridgeNameKey] = structs.NewStringAttribute(bridgeName)

	c.logger.Debug("network fingerprint complete", "bridge", bridgeName, "state", state)
}

// getBridgeState checks if the bridge interface exists and is up
func (c *Controller) getBridgeState(bridgeName string) string {
	interfaces, err := stdnet.Interfaces()
	if err != nil {
		c.logger.Error("failed to list network interfaces", "error", err)
		return NetworkStateInactive
	}

	for _, iface := range interfaces {
		if iface.Name == bridgeName {
			if iface.Flags&stdnet.FlagUp != 0 {
				return NetworkStateActive
			}
			return NetworkStateInactive
		}
	}

	c.logger.Warn("bridge not found", "bridge", bridgeName)
	return NetworkStateInactive
}

// Init performs any initialization work needed by the network sub-system
// prior to being used by the driver. This sets up the required iptables
// chains for port forwarding.
func (c *Controller) Init() error {
	return c.ensureIPTables()
}

// ensureIPTables is responsible for ensuring the local host machine iptables
// are configured with the chains and rules needed by the driver.
//
// On a new machine, this function creates the "NOMAD_CH_PRT" and "NOMAD_CH_FW"
// chains. The "NOMAD_CH_PRT" chain then has a jump rule added to the "nat"
// table; the "NOMAD_CH_FW" chain has a jump rule added to the "filter" table.
func (c *Controller) ensureIPTables() error {
	ipt, err := iptables.New()
	if err != nil {
		// In test environments or systems without iptables, skip network setup
		// Port forwarding will not work, but basic VM functionality will
		c.logger.Warn("iptables not available, skipping network setup", "error", err)
		return nil
	}

	// Ensure the NAT prerouting chain is available and create the jump rule if
	// needed.
	natCreated, err := ensureIPTablesChain(ipt, iptablesNATTableName, preroutingIPTablesChainName)
	if err != nil {
		return fmt.Errorf("failed to create iptables chain %q: %w",
			preroutingIPTablesChainName, err)
	}
	if natCreated {
		if err := ipt.Insert(iptablesNATTableName, "PREROUTING", 1, []string{"-j", preroutingIPTablesChainName}...); err != nil {
			return err
		}
		c.logger.Info("successfully created NAT prerouting iptables chain",
			"name", preroutingIPTablesChainName)
	}

	// Ensure the filter forward chain is available and create the jump rule if
	// needed.
	filterCreated, err := ensureIPTablesChain(ipt, iptablesFilterTableName, forwardIPTablesChainName)
	if err != nil {
		return fmt.Errorf("failed to create iptables chain %q: %w",
			forwardIPTablesChainName, err)
	}
	if filterCreated {
		if err := ipt.Insert(iptablesFilterTableName, "FORWARD", 1, []string{"-j", forwardIPTablesChainName}...); err != nil {
			return err
		}
		c.logger.Info("successfully created filter forward iptables chain",
			"name", forwardIPTablesChainName)
	}

	return nil
}

// ensureIPTablesChain creates an iptables chain if it doesn't exist
func ensureIPTablesChain(ipt *iptables.IPTables, table, chain string) (bool, error) {
	// List and iterate the currently configured iptables chains, so we can
	// identify whether the chain already exist.
	chains, err := ipt.ListChains(table)
	if err != nil {
		return false, err
	}
	for _, ch := range chains {
		if ch == chain {
			return false, nil
		}
	}

	err = ipt.NewChain(table, chain)

	// The error returned needs to be carefully checked as an exit code of 1
	// indicates the chain exists. This might happen when another routine has
	// created it.
	var e *iptables.Error

	if errors.As(err, &e) && e.ExitStatus() == 1 {
		return false, nil
	}

	return true, err
}

// VMStartedBuild performs network configuration once a VM has been started.
// For Cloud Hypervisor, this means setting up port forwarding rules since
// we use static IP allocation rather than DHCP discovery.
func (c *Controller) VMStartedBuild(req *net.VMStartedBuildRequest) (*net.VMStartedBuildResponse, error) {
	if req == nil {
		return nil, errors.New("net controller: no request provided")
	}
	if req.NetConfig == nil || req.Resources == nil {
		return &net.VMStartedBuildResponse{}, nil
	}

	// Dereference the network config and pull out the interface detail. The
	// driver only supports a single interface currently, so this is safe to
	// do, but when multi-interface support is added, this will need to change.
	netConfig := *req.NetConfig

	// Protect against VMs with no network interface. The log is useful for
	// debugging which certainly caught me a few times in development.
	if len(netConfig) == 0 {
		c.logger.Debug("no network interface configured", "domain", req.DomainName)
		return &net.VMStartedBuildResponse{}, nil
	}
	netInterface := netConfig[0]

	// Debug logging to see what network configuration we actually have
	c.logger.Debug("network interface configuration", "domain", req.DomainName, "netInterface", fmt.Sprintf("%+v", netInterface))
	if netInterface.Bridge != nil {
		c.logger.Debug("bridge configuration", "domain", req.DomainName, "bridge", fmt.Sprintf("%+v", netInterface.Bridge))
		c.logger.Debug("bridge name", "domain", req.DomainName, "name", netInterface.Bridge.Name)
	} else {
		c.logger.Debug("no bridge configuration found in network interface", "domain", req.DomainName)
	}

	// Determine which bridge to use - from task config if specified, otherwise from driver config
	var bridgeName string
	if netInterface.Bridge != nil && netInterface.Bridge.Name != "" {
		bridgeName = netInterface.Bridge.Name
		c.logger.Debug("using bridge from task configuration", "bridge", bridgeName)
	} else {
		bridgeName = c.networkConfig.Bridge
		c.logger.Debug("using bridge from driver configuration", "bridge", bridgeName)
	}

	// Check if the bridge interface exists
	if bridgeName != "" {
		c.logger.Debug("checking if bridge exists", "bridge", bridgeName, "domain", req.DomainName)
		if !c.bridgeExists(bridgeName) {
			c.logger.Error("bridge not found", "bridge", bridgeName, "domain", req.DomainName)
			c.logger.Error("ensure the bridge interface exists", "bridge", bridgeName, "domain", req.DomainName)
			c.logger.Error("you can create it with: sudo ip link add name", "bridge", bridgeName, "type", "bridge")
			return nil, fmt.Errorf("bridge interface %s does not exist - this should have been created during installation", bridgeName)
		}
		c.logger.Debug("bridge interface exists", "bridge", bridgeName)
	} else {
		c.logger.Error("bridge name is empty - this indicates a configuration parsing issue", "domain", req.DomainName)
		c.logger.Error("check your task configuration for network_interface.bridge.name", "domain", req.DomainName)
		return nil, fmt.Errorf("bridge name cannot be empty - check nomad-driver-ch configuration")
	}

	// Determine the guest IP address priority:
	//  1. Value provided by the virtualizer (GuestIPs)
	//  2. Static IP specified in the job configuration
	//  3. DHCP lease lookup (legacy fallback)
	var ipAddr string
	if len(req.GuestIPs) > 0 && req.GuestIPs[0] != "" {
		ipAddr = req.GuestIPs[0]
		c.logger.Debug("using IP reported by virtualizer", "ip", ipAddr, "vm", req.DomainName)
	} else if netInterface.Bridge != nil && netInterface.Bridge.StaticIP != "" {
		ipAddr = netInterface.Bridge.StaticIP
		c.logger.Debug("using static IP from task configuration", "ip", ipAddr)
	} else {
		// DHCP case - try to get IP from dnsmasq lease file as a fallback for
		// legacy environments
		mac := c.generateDeterministicMAC(req.DomainName)
		c.logger.Debug("generated deterministic MAC for DHCP", "mac", mac, "vm", req.DomainName)

		// Viper uses static IP allocation, not DHCP. Skip DHCP lease lookup and use GuestIPs directly.
		if len(req.GuestIPs) > 0 && req.GuestIPs[0] != "" {
			ipAddr = req.GuestIPs[0]
			c.logger.Info("using IP from virtualizer (static allocation)", "ip", ipAddr, "vm", req.DomainName)
		} else {
			c.logger.Error("no IP address provided by virtualizer", "vm", req.DomainName)
			return nil, fmt.Errorf("virtualizer did not provide IP address for VM %s", req.DomainName)
		}
	}

	// Configure iptables rules for port forwarding
	teardownRules, err := c.configureIPTables(req.Resources, netInterface.Bridge, ipAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to configure port mapping: %w", err)
	}

	return &net.VMStartedBuildResponse{
		DriverNetwork: &drivers.DriverNetwork{
			IP: ipAddr,
		},
		TeardownSpec: &net.TeardownSpec{
			IPTablesRules: teardownRules,
			// No DHCP reservation for CH since we use static IPs
			DHCPReservation: "",
			Network:         bridgeName,
		},
	}, nil
}

// configureIPTables is responsible for adding the iptables entries to enable
// port mapping. The function will perform this action for all configured ports
// within the network interface configuration.
//
// The returned array contains the added rules which helps make it easier to
// delete rules when a task is stopped, specifically by avoiding having to
// generate the information again.
func (c *Controller) configureIPTables(res *drivers.Resources, cfg *net.NetworkInterfaceBridgeConfig, ip string) ([][]string, error) {

	// Initialize teardown rules slice
	var teardownRules [][]string

	ipt, err := iptables.New()
	if err != nil {
		// In environments without iptables, skip port forwarding setup
		// Basic VM functionality will still work
		c.logger.Warn("iptables not available, skipping port forwarding setup", "error", err)
		return teardownRules, nil
	}

	// Create lookup mapping for ip:interface-name, so we can cache reads of
	// this and not have to perform the translation each time.
	interfaceMapping := make(map[string]string)

	// Iterate the ports configured within the network interface and pull these
	// from the task allocated ports.
	for _, port := range cfg.Ports {

		reservedPort, ok := res.Ports.Get(port)
		if !ok {
			c.logger.Error("failed to find reserved port", "port", port)
			continue
		}

		// Look into the mapping for the interface based on the host IP,
		// otherwise perform the more expensive actual lookup by querying the
		// host.
		iface, ok := interfaceMapping[reservedPort.HostIP]
		if !ok {
			iface, err = c.interfaceByIPGetter(stdnet.ParseIP(reservedPort.HostIP))
			if err != nil {
				return nil, fmt.Errorf("failed to identify IP interface: %w", err)
			}

			interfaceMapping[reservedPort.HostIP] = iface
		}

		// Generate our NAT preroute arguments to include the table and chain
		// information. This allows us to store all the detail within the
		// teardownRules easily.
		preRouteArgs := []string{
			iptablesNATTableName,
			preroutingIPTablesChainName,
			"-d", reservedPort.HostIP,
			"-i", iface,
			"-p", "tcp",
			"-m", "tcp",
			"--dport", strconv.Itoa(reservedPort.Value),
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("%s:%v", ip, reservedPort.To),
		}

		if err := ipt.Append(preRouteArgs[0], preRouteArgs[1], preRouteArgs[2:]...); err != nil {
			return nil, err
		}

		c.logger.Debug("configured nat prerouting chain", "args", preRouteArgs)
		teardownRules = append(teardownRules, preRouteArgs)

		// Generate our filter forward arguments to include the table and chain
		// information. This allows us to store all the detail within the
		// teardownRules easily.
		filterArgs := []string{
			iptablesFilterTableName,
			forwardIPTablesChainName,
			"-d", ip,
			"-p", "tcp",
			"-m", "state",
			"--state", "NEW",
			"-m", "tcp",
			"--dport", strconv.Itoa(reservedPort.To),
			"-j", "ACCEPT",
		}

		if err := ipt.Append(filterArgs[0], filterArgs[1], filterArgs[2:]...); err != nil {
			return nil, err
		}

		c.logger.Debug("configured filter forward chain", "args", filterArgs)
		teardownRules = append(teardownRules, filterArgs)

		// The process made a change to the system, so log the critical
		// information that might be useful to operators.
		c.logger.Info("successfully configured port forwarding rules",
			"src_ip", reservedPort.HostIP, "src_port", reservedPort.Value,
			"dst_ip", ip, "dst_port", reservedPort.To, "port_label", port)
	}

	return teardownRules, nil
}

// getInterfaceByIP is a helper function which identifies which host network
// interface the passed IP address is linked to.
func getInterfaceByIP(ip stdnet.IP) (string, error) {
	interfaces, err := stdnet.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range interfaces {
		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				if iip, _, err := stdnet.ParseCIDR(addr.String()); err == nil {
					if iip.Equal(ip) {
						return iface.Name, nil
					}
				} else {
					continue
				}
			}
		} else {
			continue
		}
	}
	return "", fmt.Errorf("failed to find interface for IP %q", ip.String())
}

// VMTerminatedTeardown performs all the network teardown required to clean
// the host and any systems of configuration specific to the task.
func (c *Controller) VMTerminatedTeardown(req *net.VMTerminatedTeardownRequest) (*net.VMTerminatedTeardownResponse, error) {
	// We can't be exactly sure what the caller will give us, so make sure we
	// don't panic the driver.
	if req == nil || req.TeardownSpec == nil {
		return &net.VMTerminatedTeardownResponse{}, nil
	}

	ipt, err := iptables.New()
	if err != nil {
		// In environments without iptables, skip network teardown
		// This is not ideal but allows the system to continue
		c.logger.Warn("iptables not available, skipping network teardown", "error", err)
		return &net.VMTerminatedTeardownResponse{}, nil
	}

	// Collect all the errors, so we provide the operator with enough
	// information to manually tidy if needed.
	var mErr multierror.Error

	// Iterate the teardown rules and delete them from iptables. Do not halt
	// the loop if we encounter an error, track it and plough forward, so we
	// attempt to clean up as much as possible.
	//
	// Using DeleteIfExists means we do not generate error if the rule does not
	// exist. This is important for partial failure scenarios where we delete
	// one or more rules and one or more fail. The client will retry the
	// stop/kill call until all work is completed successfully. If we return an
	// error if the rule is not found, we can never recover from partial
	// failures.
	for _, iptablesRule := range req.TeardownSpec.IPTablesRules {
		if err := ipt.DeleteIfExists(iptablesRule[0], iptablesRule[1], iptablesRule[2:]...); err != nil {
			mErr.Errors = append(
				mErr.Errors,
				fmt.Errorf("failed to delete iptables %q entry in %q chain: %w",
					iptablesRule[0], iptablesRule[1], err))
		}
	}

	// No DHCP reservation cleanup needed for Cloud Hypervisor since we use static IPs

	return &net.VMTerminatedTeardownResponse{}, mErr.ErrorOrNil()
}

func ipv4ToUint32(addr netip.Addr) uint32 {
	b := addr.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func uint32ToIPv4(val uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{
		byte(val >> 24),
		byte(val >> 16),
		byte(val >> 8),
		byte(val),
	})
}

// generateDeterministicMAC generates a deterministic MAC address from domain name
// This ensures consistent MAC assignment for the same domain
func (c *Controller) generateDeterministicMAC(domainName string) string {
	// Use a simple hash-based approach to generate a deterministic MAC
	// This ensures the same domain name always gets the same MAC address
	hash := 0
	for _, char := range domainName {
		hash = hash*31 + int(char)
	}

	// Generate MAC with 52:54:00 prefix (QEMU/KVM range)
	// Use the hash to generate the last 3 bytes
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x",
		byte(hash>>16), byte(hash>>8), byte(hash))
}

// lookupDHCPLeaseByMAC looks up the IP address for a given MAC in dnsmasq lease file
func (c *Controller) lookupDHCPLeaseByMAC(mac string) (string, error) {
	leaseFile := defaultDnsmasqLeaseFile

	// Read the dnsmasq lease file
	file, err := os.Open(leaseFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("dnsmasq lease file not found at %s - ensure dnsmasq is installed and running with DHCP enabled", leaseFile)
		}
		return "", fmt.Errorf("failed to open dnsmasq lease file %s: %w", leaseFile, err)
	}
	defer file.Close()

	// Parse dnsmasq lease file format: expiry IP MAC hostname client-id
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) < 3 {
			continue
		}

		// Check if MAC matches (dnsmasq stores MAC without colons)
		leaseMAC := strings.ReplaceAll(fields[2], ":", "")
		targetMAC := strings.ReplaceAll(mac, ":", "")

		if leaseMAC == targetMAC {
			// Check if lease is still active (expiry > current time)
			expiry, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				continue
			}

			if time.Now().Unix() < expiry {
				c.logger.Debug("found active DHCP lease", "ip", fields[1], "mac", mac)
				return fields[1], nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading dnsmasq lease file: %w", err)
	}

	return "", fmt.Errorf("no active lease found for MAC %s", mac)
}

// bridgeExists checks if a bridge interface exists on the system
func (c *Controller) bridgeExists(bridgeName string) bool {
	// Check if the bridge interface actually exists
	if bridgeName == "" {
		return false
	}

	// Use ip command to check if bridge exists
	cmd := exec.Command("ip", "link", "show", bridgeName)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
