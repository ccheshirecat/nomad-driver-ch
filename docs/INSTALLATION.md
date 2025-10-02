# Installation Guide

This guide provides detailed installation instructions for the Nomad Cloud Hypervisor driver.

## System Requirements

### Minimum Requirements
- **OS**: Linux (Ubuntu 20.04+, RHEL 8+, CentOS 8+, or compatible)
- **CPU**: x86_64 with Intel VT-x or AMD-V virtualization support
- **Memory**: 4GB RAM (additional memory for VMs)
- **Storage**: 20GB free disk space (additional space for VM images)
- **Network**: Bridge networking capability

### Software Dependencies
- **Nomad**: v1.4.0 or later
- **Cloud Hypervisor**: v48.0.0 or later
- **Linux Kernel**: v5.4.0 or later with KVM support
- **VirtioFS daemon**: virtiofsd package

### Hardware Verification

Check virtualization support:
```bash
# Check CPU virtualization features
egrep -o '(vmx|svm)' /proc/cpuinfo

# Verify KVM modules are loaded
lsmod | grep -E '(kvm_intel|kvm_amd)'

# Check /dev/kvm exists and is accessible
ls -la /dev/kvm
```

If KVM modules are not loaded:
```bash
# Load KVM modules
sudo modprobe kvm_intel  # For Intel CPUs
# OR
sudo modprobe kvm_amd    # For AMD CPUs

# Make modules load on boot
echo 'kvm_intel' | sudo tee -a /etc/modules
# OR
echo 'kvm_amd' | sudo tee -a /etc/modules
```

## Step-by-Step Installation

### 1. Install Cloud Hypervisor

**Option A: Download Binary Releases**
```bash
# Set version
CH_VERSION="v48.0"

# Download Cloud Hypervisor
wget "https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/${CH_VERSION}/cloud-hypervisor-static"
sudo mv cloud-hypervisor-static /usr/local/bin/cloud-hypervisor
sudo chmod +x /usr/local/bin/cloud-hypervisor

# Download ch-remote
wget "https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/${CH_VERSION}/ch-remote-static"
sudo mv ch-remote-static /usr/local/bin/ch-remote
sudo chmod +x /usr/local/bin/ch-remote

# Ensure binaries are in PATH for the current session
export PATH="/usr/local/bin:$PATH"

# (Optional) verify glibc requirements if using dynamically-linked binaries
strings /usr/local/bin/cloud-hypervisor | grep GLIBC_

# Verify installation
cloud-hypervisor --version
ch-remote --version
```

**Option B: Build from Source**
```bash
# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source ~/.cargo/env

# Clone and build
git clone https://github.com/cloud-hypervisor/cloud-hypervisor.git
cd cloud-hypervisor
git checkout v48.0.0
cargo build --release --bin cloud-hypervisor --bin ch-remote

# Install binaries
sudo cp target/release/cloud-hypervisor /usr/local/bin/
sudo cp target/release/ch-remote /usr/local/bin/
sudo chmod +x /usr/local/bin/cloud-hypervisor /usr/local/bin/ch-remote
```

### 2. Install VirtioFS Daemon

**Ubuntu/Debian:**
```bash
sudo apt-get update
sudo apt-get install -y virtiofsd
```

**RHEL/CentOS/Fedora:**
```bash
# RHEL 8+/CentOS 8+
sudo dnf install -y virtiofsd

# Older versions
sudo yum install -y virtiofsd
```

**Build from Source (if package unavailable):**
```bash
# Install dependencies
sudo apt-get install -y git build-essential pkg-config libcap-ng-dev libseccomp-dev

# Clone and build
git clone https://gitlab.com/virtio-fs/virtiofsd.git
cd virtiofsd
cargo build --release

# Install
sudo cp target/release/virtiofsd /usr/bin/
sudo chmod +x /usr/bin/virtiofsd
```

### 3. Configure Bridge Networking

**Method 1: Using systemd-networkd (Recommended)**

Create bridge network device:
```bash
# Create bridge netdev configuration
sudo tee /etc/systemd/network/br0.netdev << EOF
[NetDev]
Name=br0
Kind=bridge
EOF

# Create bridge network configuration
sudo tee /etc/systemd/network/br0.network << EOF
[Match]
Name=br0

[Network]
IPForward=yes
Address=192.168.1.1/24
DHCPServer=yes

[DHCPServer]
PoolOffset=100
PoolSize=100
DefaultLeaseTimeSec=3600
EOF

# Enable and restart systemd-networkd
sudo systemctl enable systemd-networkd
sudo systemctl restart systemd-networkd
```

**Method 2: Using NetworkManager**

```bash
# Create bridge
sudo nmcli connection add type bridge con-name br0 ifname br0

# Configure IP
sudo nmcli connection modify br0 ipv4.addresses 192.168.1.1/24
sudo nmcli connection modify br0 ipv4.method manual

# Activate bridge
sudo nmcli connection up br0
```

**Method 3: Manual Configuration**

```bash
# Create bridge
sudo ip link add br0 type bridge
sudo ip addr add 192.168.1.1/24 dev br0
sudo ip link set br0 up

# Make persistent (add to /etc/rc.local or init scripts)
cat >> /etc/rc.local << EOF
ip link add br0 type bridge
ip addr add 192.168.1.1/24 dev br0
ip link set br0 up
EOF
```

**Verify bridge configuration:**
```bash
ip link show br0
ip addr show br0
```

### 4. Install Nomad Driver Plugin

**Option A: Download Pre-built Binary**
```bash
# Download latest release
PLUGIN_VERSION="latest" # or specify version like "v1.0.0"
wget "https://github.com/ccheshirecat/nomad-driver-ch/releases/${PLUGIN_VERSION}/download/nomad-driver-ch"

# Install plugin
sudo mkdir -p /opt/nomad/plugins
sudo mv nomad-driver-ch /opt/nomad/plugins/
sudo chmod +x /opt/nomad/plugins/nomad-driver-ch
```

**Option B: Build from Source**
```bash
# Install Go (if not already installed)
wget https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Clone and build
git clone https://github.com/ccheshirecat/nomad-driver-ch.git
cd nomad-driver-ch
go mod download
go build -o nomad-driver-ch .

# Install plugin
sudo mkdir -p /opt/nomad/plugins
sudo cp nomad-driver-ch /opt/nomad/plugins/
sudo chmod +x /opt/nomad/plugins/nomad-driver-ch
```

### 5. Configure Nomad Client

Create or update Nomad client configuration:

```bash
sudo mkdir -p /etc/nomad.d
sudo tee /etc/nomad.d/client.hcl << EOF
# Nomad client configuration
datacenter = "dc1"
data_dir = "/opt/nomad/data"
log_level = "INFO"
node_name = "nomad-client"

client {
  enabled = true

  # Node class for scheduling constraints
  node_class = "vm-capable"

  # Plugin directory
  plugin_dir = "/opt/nomad/plugins"

  # Cloud Hypervisor driver configuration
  plugin "nomad-driver-ch" {
    config {
      # Cloud Hypervisor binaries
      cloud_hypervisor {
        bin = "/usr/bin/cloud-hypervisor"
        remote_bin = "/usr/bin/ch-remote"
        virtiofsd_bin = "/usr/bin/virtiofsd"
        default_kernel = "/boot/vmlinuz"
        default_initramfs = "/boot/initramfs.img"
        seccomp = "true"
        log_file = "/var/log/cloud-hypervisor.log"
      }

      # Network configuration
      network {
        bridge = "br0"
        subnet_cidr = "192.168.1.0/24"
        gateway = "192.168.1.1"
        ip_pool_start = "192.168.1.100"
        ip_pool_end = "192.168.1.200"
        tap_prefix = "tap"
      }

      # VFIO configuration (optional)
      vfio {
        allowlist = ["10de:*"]  # Allow NVIDIA GPUs
        iommu_address_width = 48
        pci_segments = 1
      }

      # Security: allowed image paths
      image_paths = [
        "/var/lib/vm-images",
        "/opt/vm-images",
        "/mnt/shared-storage/images"
      ]

      # Working directory
      data_dir = "/opt/nomad/data"
    }
  }
}

# Enable debug logging for troubleshooting (optional)
log_level = "DEBUG"
enable_debug = true
EOF
```

### 6. Create VM Image Directory

```bash
# Create image storage directory
sudo mkdir -p /var/lib/vm-images
sudo chmod 755 /var/lib/vm-images

# Create working directory for Nomad
sudo mkdir -p /opt/nomad/data
sudo chown nomad:nomad /opt/nomad/data
```

### 7. Start and Verify Nomad

```bash
# Start Nomad service
sudo systemctl start nomad
sudo systemctl enable nomad

# Check status
sudo systemctl status nomad

# Verify driver is loaded
nomad node status -self | grep -A 5 "Driver Status"

# Check for Cloud Hypervisor driver
nomad node status -detailed $(nomad node status -self | grep "Node ID" | awk '{print $4}') | grep virt
```

## Post-Installation Setup

### 1. Download Sample VM Images

```bash
# Create image directory
sudo mkdir -p /var/lib/vm-images
cd /var/lib/vm-images

# Download Ubuntu Cloud Image
sudo wget https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img

# Download Alpine Cloud Image
sudo wget https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/cloud/alpine-3.18.4-x86_64-bios-cloudinit-r0.qcow2

# Set permissions
sudo chmod 644 *.img *.qcow2
```

### 2. Test Installation

Create a test job:

```bash
cat > test-vm.nomad.hcl << EOF
job "test-vm" {
  datacenters = ["dc1"]
  type = "batch"

  group "test" {
    task "alpine-test" {
      driver = "virt"

      config {
        image = "/var/lib/vm-images/alpine-3.18.4-x86_64-bios-cloudinit-r0.qcow2"
        hostname = "test-alpine"

        default_user_password = "testpass"

        cmds = [
          "echo 'Hello from Cloud Hypervisor VM!'",
          "uname -a",
          "free -m"
        ]
      }

      resources {
        cpu    = 1000
        memory = 512
      }
    }
  }
}
EOF

# Run test job
nomad job run test-vm.nomad.hcl

# Check status
nomad job status test-vm
nomad alloc logs $(nomad job allocs test-vm | tail -n 1 | awk '{print $1}')
```

## Troubleshooting Installation

### Common Issues

**1. Cloud Hypervisor not found**
```bash
# Verify binary exists and is executable
ls -la /usr/bin/cloud-hypervisor
which cloud-hypervisor

# Check PATH
echo $PATH
```

**2. KVM not accessible**
```bash
# Check KVM device permissions
ls -la /dev/kvm

# Add user to kvm group (if running Nomad as non-root)
sudo usermod -a -G kvm nomad
```

**3. Bridge networking issues**
```bash
# Check bridge status
ip link show br0
brctl show  # if bridge-utils installed

# Test bridge connectivity
ping 192.168.1.1
```

**4. Plugin not loading**
```bash
# Check plugin file
ls -la /opt/nomad/plugins/nomad-driver-ch

# Check Nomad logs
journalctl -u nomad -f

# Verify plugin configuration
nomad agent-info | grep -A 20 "nomad-driver-ch"
```

### Log Locations

- **Nomad logs**: `journalctl -u nomad` or `/var/log/nomad.log`
- **Cloud Hypervisor logs**: `/var/log/cloud-hypervisor.log` (if configured)
- **VM console logs**: `/opt/nomad/data/alloc/<alloc-id>/<task>/serial.log`
- **Cloud-init logs** (inside VM): `/var/log/cloud-init.log`

### Performance Tuning

**1. Enable Huge Pages**
```bash
# Enable 2MB huge pages
echo 1024 | sudo tee /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages

# Make persistent
echo 'vm.nr_hugepages=1024' | sudo tee -a /etc/sysctl.conf
```

**2. CPU Isolation (for dedicated VM hosts)**
```bash
# Isolate CPUs for VM use (example: isolate CPUs 2-7)
# Add to GRUB_CMDLINE_LINUX in /etc/default/grub
GRUB_CMDLINE_LINUX="isolcpus=2-7 nohz_full=2-7 rcu_nocbs=2-7"

sudo update-grub
sudo reboot
```

**3. Storage Optimization**
```bash
# Use faster storage for VM images and Nomad data
sudo mkdir -p /fast-storage/vm-images
sudo mkdir -p /fast-storage/nomad-data

# Update paths in Nomad configuration
# image_paths = ["/fast-storage/vm-images"]
# data_dir = "/fast-storage/nomad-data"
```

## Security Considerations

### 1. File Permissions
```bash
# Ensure secure permissions
sudo chmod 755 /opt/nomad/plugins/nomad-driver-ch
sudo chmod 644 /etc/nomad.d/*.hcl
sudo chmod 700 /opt/nomad/data
```

### 2. Network Security
```bash
# Configure firewall for VM network
sudo ufw allow in on br0
sudo ufw deny in on br0 to 192.168.1.1

# Block inter-VM communication (optional)
sudo iptables -I FORWARD -i br0 -o br0 -j DROP
sudo iptables -I FORWARD -i br0 -o br0 -m state --state ESTABLISHED,RELATED -j ACCEPT
```

### 3. Resource Limits
```bash
# Set ulimits for nomad user
echo "nomad soft nofile 65536" | sudo tee -a /etc/security/limits.conf
echo "nomad hard nofile 65536" | sudo tee -a /etc/security/limits.conf
```

## Next Steps

After successful installation:

1. **Read Configuration Guide**: [docs/CONFIGURATION.md](CONFIGURATION.md)
2. **Try Task Examples**: [docs/EXAMPLES.md](EXAMPLES.md)
3. **Setup Monitoring**: [docs/MONITORING.md](MONITORING.md)
4. **Review Security**: [docs/SECURITY.md](SECURITY.md)

## Getting Help

- **Documentation**: Check [docs/](../) directory for detailed guides
- **Issues**: Report problems at [GitHub Issues](https://github.com/ccheshirecat/nomad-driver-ch/issues)
- **Community**: Join discussions in GitHub Discussions