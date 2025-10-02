# Configuration Reference

This document provides detailed configuration reference for the Nomad Cloud Hypervisor driver.

## Table of Contents

- [Driver Configuration](#driver-configuration)
- [Task Configuration](#task-configuration)
- [Network Configuration](#network-configuration)
- [Resource Configuration](#resource-configuration)
- [Cloud-Init Configuration](#cloud-init-configuration)
- [VFIO Device Passthrough](#vfio-device-passthrough)
- [Storage Configuration](#storage-configuration)
- [Security Configuration](#security-configuration)
- [Performance Tuning](#performance-tuning)

## Driver Configuration

The driver configuration is specified in the Nomad client configuration file under the `plugin` block.

### Complete Configuration Example

```hcl
plugin "nomad-driver-ch" {
  config {
    # Cloud Hypervisor binary configuration
    cloud_hypervisor {
      bin = "/usr/bin/cloud-hypervisor"
      remote_bin = "/usr/bin/ch-remote"
      virtiofsd_bin = "/usr/bin/virtiofsd"
      default_kernel = "/boot/vmlinuz-5.15.0"
      default_initramfs = "/boot/initramfs-5.15.0.img"
      firmware = "/usr/share/ovmf/OVMF.fd"
      seccomp = "true"
      log_file = "/var/log/cloud-hypervisor.log"
      disable_alloc_mounts = false
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

    # VFIO device passthrough
    vfio {
      allowlist = ["10de:*", "8086:0d26"]
      iommu_address_width = 48
      pci_segments = 1
    }

    # Data and image paths
    data_dir = "/opt/nomad/data"
    image_paths = [
      "/var/lib/vm-images",
      "/opt/shared-images",
      "/mnt/nfs/images"
    ]
  }
}
```

### Cloud Hypervisor Configuration Block

#### `cloud_hypervisor.bin`
- **Type**: `string`
- **Default**: `"/usr/bin/cloud-hypervisor"`
- **Description**: Path to the Cloud Hypervisor binary
- **Example**:
  ```hcl
  bin = "/opt/cloud-hypervisor/bin/cloud-hypervisor"
  ```

#### `cloud_hypervisor.remote_bin`
- **Type**: `string`
- **Default**: `"/usr/bin/ch-remote"`
- **Description**: Path to the ch-remote management binary
- **Example**:
  ```hcl
  remote_bin = "/opt/cloud-hypervisor/bin/ch-remote"
  ```

#### `cloud_hypervisor.virtiofsd_bin`
- **Type**: `string`
- **Default**: `"/usr/bin/virtiofsd"`
- **Description**: Path to the virtiofsd daemon for filesystem sharing
- **Example**:
  ```hcl
  virtiofsd_bin = "/usr/libexec/virtiofsd"
  ```

#### `cloud_hypervisor.default_kernel`
- **Type**: `string`
- **Default**: None (required)
- **Description**: Default kernel image path for VMs
- **Example**:
  ```hcl
  default_kernel = "/boot/vmlinuz-5.15.0-cloud"
  ```

#### `cloud_hypervisor.default_initramfs`
- **Type**: `string`
- **Default**: None (required)
- **Description**: Default initramfs image path for VMs
- **Example**:
  ```hcl
  default_initramfs = "/boot/initramfs-5.15.0-cloud.img"
  ```

#### `cloud_hypervisor.firmware`
- **Type**: `string`
- **Default**: None (optional)
- **Description**: UEFI firmware path for EFI boot
- **Example**:
  ```hcl
  firmware = "/usr/share/ovmf/OVMF.fd"
  ```

#### `cloud_hypervisor.seccomp`
- **Type**: `string`
- **Default**: `"true"`
- **Description**: Enable seccomp filtering for improved security
- **Options**: `"true"`, `"false"`, or path to custom seccomp filter
- **Example**:
  ```hcl
  seccomp = "/etc/cloud-hypervisor/seccomp.json"
  ```

#### `cloud_hypervisor.log_file`
- **Type**: `string`
- **Default**: None (optional)
- **Description**: Path for Cloud Hypervisor log output
- **Example**:
  ```hcl
  log_file = "/var/log/nomad/cloud-hypervisor.log"
  ```

### Network Configuration Block

#### `network.bridge`
- **Type**: `string`
- **Default**: `"br0"`
- **Description**: Bridge interface name for VM networking
- **Example**:
  ```hcl
  bridge = "nomad-br0"
  ```

#### `network.subnet_cidr`
- **Type**: `string`
- **Default**: `"192.168.1.0/24"`
- **Description**: Subnet CIDR for VM IP allocation
- **Example**:
  ```hcl
  subnet_cidr = "10.0.0.0/16"
  ```

#### `network.gateway`
- **Type**: `string`
- **Default**: `"192.168.1.1"`
- **Description**: Gateway IP address for VMs
- **Example**:
  ```hcl
  gateway = "10.0.0.1"
  ```

#### `network.ip_pool_start`
- **Type**: `string`
- **Default**: `"192.168.1.100"`
- **Description**: Start of IP allocation pool
- **Example**:
  ```hcl
  ip_pool_start = "10.0.10.1"
  ```

#### `network.ip_pool_end`
- **Type**: `string`
- **Default**: `"192.168.1.200"`
- **Description**: End of IP allocation pool
- **Example**:
  ```hcl
  ip_pool_end = "10.0.10.254"
  ```

#### `network.tap_prefix`
- **Type**: `string`
- **Default**: `"tap"`
- **Description**: Prefix for TAP interface names
- **Example**:
  ```hcl
  tap_prefix = "nomad-tap"
  ```

### Additional Driver Flags

#### `disable_alloc_mounts`
- **Type**: `bool`
- **Default**: `false`
- **Description**: When enabled, the driver skips mounting Nomad allocation directories (`alloc`, `local`, `secrets`) over virtio-fs. Useful for hardened environments or when those mounts are managed externally.
- **Example**:
  ```hcl
  disable_alloc_mounts = true
  ```

### VFIO Configuration Block

#### `vfio.allowlist`
- **Type**: `[]string`
- **Default**: `[]` (empty)
- **Description**: List of allowed PCI device IDs for passthrough
- **Format**: Vendor:Device (wildcards supported)
- **Example**:
  ```hcl
  allowlist = [
    "10de:*",      # All NVIDIA devices
    "8086:0d26",   # Specific Intel device
    "1002:67df"    # AMD Radeon RX 480
  ]
  ```

#### `vfio.iommu_address_width`
- **Type**: `number`
- **Default**: None (optional)
- **Description**: IOMMU address width in bits
- **Example**:
  ```hcl
  iommu_address_width = 48
  ```

#### `vfio.pci_segments`
- **Type**: `number`
- **Default**: None (optional)
- **Description**: Number of PCI segments
- **Example**:
  ```hcl
  pci_segments = 1
  ```

### Path Configuration

#### `data_dir`
- **Type**: `string`
- **Default**: Nomad data directory
- **Description**: Working directory for plugin data
- **Example**:
  ```hcl
  data_dir = "/var/lib/nomad-ch"
  ```

#### `image_paths`
- **Type**: `[]string`
- **Default**: `[data_dir]`
- **Description**: Allowed paths for VM disk images (security)
- **Example**:
  ```hcl
  image_paths = [
    "/var/lib/vm-images",
    "/opt/shared-storage/images",
    "/mnt/nfs-images"
  ]
  ```

## Task Configuration

Task configuration is specified in the `config` block of a task using the `virt` driver.

### Complete Task Configuration Example

```hcl
task "example-vm" {
  driver = "virt"

  config {
    # Required: Disk image
    image = "/var/lib/images/ubuntu-22.04.img"

    # VM identification
    hostname = "my-vm"

    # Operating system configuration
    os {
      arch    = "x86_64"
      machine = "q35"
      variant = "ubuntu22.04"
    }

    # Cloud-init configuration
    user_data = "/etc/cloud-init/setup.yml"
    default_user_password = "secure-password"
    default_user_authorized_ssh_key = "ssh-rsa AAAAB3..."

    # Custom commands
    cmds = [
      "apt-get update",
      "systemctl enable my-service"
    ]

    # Timezone
    timezone = "America/New_York"

    # Storage configuration
    use_thin_copy = true

    # Cloud Hypervisor specific
    kernel = "/boot/custom-kernel"
    initramfs = "/boot/custom-initrd"
    cmdline = "console=ttyS0 quiet"

    # Network interface
    network_interface {
      bridge {
        name = "br0"
        static_ip = "192.168.1.150"
        gateway = "192.168.1.1"
        netmask = "24"
        dns = ["8.8.8.8", "1.1.1.1"]
        ports = ["web", "api"]
      }
    }

    # Device passthrough
    vfio_devices = ["10de:2204"]
    usb_devices = ["046d:c52b"]
  }

  resources {
    cpu    = 2000
    memory = 2048
  }
}
```

### Core Configuration

#### `image`
- **Type**: `string`
- **Required**: Yes
- **Description**: Path to VM disk image
- **Supported formats**: RAW (.img), QCOW2 (.qcow2), VHD (.vhd), VMDK (.vmdk)
- **Example**:
  ```hcl
  image = "/var/lib/images/ubuntu-22.04.img"
  ```

#### `hostname`
- **Type**: `string`
- **Default**: Generated UUID
- **Description**: VM hostname
- **Example**:
  ```hcl
  hostname = "web-server-${NOMAD_ALLOC_INDEX}"
  ```

### Operating System Configuration

#### `os.arch`
- **Type**: `string`
- **Default**: Host architecture
- **Description**: CPU architecture
- **Options**: `"x86_64"`, `"aarch64"`
- **Example**:
  ```hcl
  os {
    arch = "x86_64"
  }
  ```

#### `os.machine`
- **Type**: `string`
- **Default**: `"q35"`
- **Description**: Machine type
- **Options**: `"q35"`, `"pc"`, `"virt"` (ARM)
- **Example**:
  ```hcl
  os {
    machine = "q35"
  }
  ```

#### `os.variant`
- **Type**: `string`
- **Default**: Auto-detected
- **Description**: OS variant for optimization
- **Example**:
  ```hcl
  os {
    variant = "ubuntu22.04"
  }
  ```

### Cloud-Init Configuration

#### `user_data`
- **Type**: `string`
- **Default**: None
- **Description**: Cloud-init user data (file path or inline YAML)
- **Example**:
  ```hcl
  user_data = "/etc/cloud-init/web-setup.yml"
  # OR
  user_data = <<EOF
  #cloud-config
  packages:
    - nginx
  EOF
  ```

#### `default_user_password`
- **Type**: `string`
- **Default**: None
- **Description**: Default user password
- **Example**:
  ```hcl
  default_user_password = "secure123"
  ```

#### `default_user_authorized_ssh_key`
- **Type**: `string`
- **Default**: None
- **Description**: SSH public key for default user
- **Example**:
  ```hcl
  default_user_authorized_ssh_key = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQAB..."
  ```

#### `cmds`
- **Type**: `[]string`
- **Default**: `[]`
- **Description**: Commands to run during VM setup
- **Example**:
  ```hcl
  cmds = [
    "apt-get update",
    "systemctl enable nginx",
    "ufw enable"
  ]
  ```

#### `timezone`
- **Type**: `string`
- **Default**: UTC
- **Description**: VM timezone
- **Example**:
  ```hcl
  timezone = "Europe/London"
  ```

### Storage Configuration

#### `use_thin_copy`
- **Type**: `bool`
- **Default**: `false`
- **Description**: Use copy-on-write for faster startup
- **Example**:
  ```hcl
  use_thin_copy = true
  ```


### Cloud Hypervisor Specific

#### `kernel`
- **Type**: `string`
- **Default**: Driver default kernel
- **Description**: Custom kernel path
- **Example**:
  ```hcl
  kernel = "/boot/vmlinuz-custom"
  ```

#### `initramfs`
- **Type**: `string`
- **Default**: Driver default initramfs
- **Description**: Custom initramfs path
- **Example**:
  ```hcl
  initramfs = "/boot/initramfs-custom.img"
  ```

#### `cmdline`
- **Type**: `string`
- **Default**: `"console=ttyS0 root=/dev/vda1"`
- **Description**: Kernel command line parameters
- **Example**:
  ```hcl
  cmdline = "console=ttyS0 root=/dev/vda1 quiet"
  ```

### Optional Binary Validation Controls

#### `skip_binary_validation`
- **Type**: `bool`
- **Default**: `false`
- **Description**: Available via SDK helpers primarily for testing. When set, the driver bypasses host binary checks (Cloud Hypervisor, virtiofsd, `brctl/ip`) during initialization. Production deployments should keep validation enabled to surface misconfiguration early.
- **Example**:
  ```hcl
  skip_binary_validation = true
  ```

## Network Configuration

### Network Interface Configuration

#### `network_interface.bridge`
Network interface configuration for bridge networking.

```hcl
network_interface {
  bridge {
    name = "br0"                      # Required
    static_ip = "192.168.1.100"       # Optional
    gateway = "192.168.1.1"           # Optional
    netmask = "24"                    # Optional
    dns = ["8.8.8.8", "1.1.1.1"]     # Optional
    ports = ["web", "api"]            # Optional
  }
}
```

##### `name`
- **Type**: `string`
- **Required**: Yes
- **Description**: Bridge interface name
- **Example**: `"br0"`

##### `static_ip`
- **Type**: `string`
- **Default**: Auto-allocated from pool
- **Description**: Static IP address for VM
- **Example**: `"192.168.1.100"`

##### `gateway`
- **Type**: `string`
- **Default**: Driver gateway
- **Description**: Custom gateway for this interface
- **Example**: `"192.168.1.1"`

##### `netmask`
- **Type**: `string`
- **Default**: Driver subnet mask
- **Description**: Subnet mask in CIDR notation
- **Example**: `"24"`

##### `dns`
- **Type**: `[]string`
- **Default**: `["8.8.8.8", "8.8.4.4"]`
- **Description**: DNS servers for this interface
- **Example**: `["1.1.1.1", "8.8.8.8"]`

##### `ports`
- **Type**: `[]string`
- **Default**: `[]`
- **Description**: Port labels to expose from network block
- **Example**: `["web", "api", "ssh"]`

## Resource Configuration

Resource configuration defines CPU, memory, and device allocation for VMs.

### CPU Configuration

```hcl
resources {
  cpu = 2000  # CPU shares (1000 = 1 core)
}
```

- **Type**: `number`
- **Units**: CPU shares (1000 shares = 1 CPU core)
- **Example**:
  - `cpu = 1000` # 1 CPU core
  - `cpu = 2500` # 2.5 CPU cores

### Memory Configuration

```hcl
resources {
  memory = 2048  # Memory in MB
}
```

- **Type**: `number`
- **Units**: Megabytes
- **Example**:
  - `memory = 512`  # 512MB
  - `memory = 4096` # 4GB

### Device Configuration

```hcl
resources {
  device "nvidia/gpu" {
    count = 1
    constraint {
      attribute = "${device.attr.compute_capability}"
      operator  = ">="
      value     = "6.0"
    }
  }
}
```

## Cloud-Init Configuration

### Built-in Cloud-Init Features

The driver automatically generates cloud-init configuration based on task settings:

#### User Management
- Password configuration
- SSH key setup
- User creation

#### Network Configuration
- Static IP or DHCP
- Gateway and DNS setup
- Multi-interface support

#### Command Execution
- Boot commands (`cmds` field)
- Run commands
- Package installation

#### File Management
- Filesystem mounts (VirtioFS)
- File creation and permissions

### Custom User Data

You can provide custom cloud-init configuration:

#### File-based User Data
```hcl
config {
  user_data = "/path/to/user-data.yml"
}
```

#### Inline User Data
```hcl
config {
  user_data = <<EOF
#cloud-config
packages:
  - docker.io
  - nginx

users:
  - name: myuser
    groups: sudo
    shell: /bin/bash
    ssh_authorized_keys:
      - ssh-rsa AAAAB3...

write_files:
  - path: /etc/docker/daemon.json
    content: |
      {
        "log-driver": "json-file",
        "log-opts": {
          "max-size": "10m"
        }
      }
    permissions: '0644'

runcmd:
  - systemctl enable docker
  - systemctl start docker
  - usermod -aG docker myuser
EOF
}
```

## VFIO Device Passthrough

### PCI Device Passthrough

#### `vfio_devices`
- **Type**: `[]string`
- **Description**: List of PCI device IDs to pass through
- **Format**: `vendor:device` (as shown by `lspci -nn`)
- **Example**:
  ```hcl
  vfio_devices = [
    "10de:2204",  # NVIDIA RTX 3080
    "10de:1aef"   # NVIDIA Audio Controller
  ]
  ```

### USB Device Passthrough

#### `usb_devices`
- **Type**: `[]string`
- **Description**: List of USB device IDs to pass through
- **Format**: `vendor:product` (as shown by `lsusb`)
- **Example**:
  ```hcl
  usb_devices = [
    "046d:c52b",  # Logitech webcam
    "0b05:1872"   # ASUS USB adapter
  ]
  ```

### Host Configuration for VFIO

Enable VFIO on the host system:

#### GRUB Configuration
```bash
# Edit /etc/default/grub
GRUB_CMDLINE_LINUX="intel_iommu=on vfio-pci.ids=10de:2204,10de:1aef"

# Update and reboot
sudo update-grub
sudo reboot
```

#### Verify VFIO Setup
```bash
# Check IOMMU groups
find /sys/kernel/iommu_groups/ -name "devices" -exec ls -la {} \;

# Verify device binding
lspci -nnk -d 10de:2204
```

## Storage Configuration

### Volume Mounts

Mount host directories into VMs:

```hcl
# In group block
volume "data" {
  type      = "host"
  source    = "app-data"
  read_only = false
}

# In task block
volume_mount {
  volume      = "data"
  destination = "/app/data"
  read_only   = false
}
```

### Host Volume Configuration

Define host volumes in Nomad client configuration:

```hcl
client {
  host_volume "app-data" {
    path      = "/opt/app-data"
    read_only = false
  }
}
```

## Security Configuration

### Image Path Restrictions

Restrict allowed image paths for security:

```hcl
config {
  image_paths = [
    "/var/lib/secure-images",
    "/opt/trusted-images"
  ]
}
```

### Seccomp Filtering

Enable seccomp for additional security:

```hcl
config {
  cloud_hypervisor {
    seccomp = "true"  # Use built-in filter
    # OR
    seccomp = "/etc/ch/custom-seccomp.json"  # Custom filter
  }
}
```

### Network Security

Configure firewall rules for VM networking:

```bash
# Allow VM traffic on bridge
sudo iptables -A FORWARD -i br0 -j ACCEPT

# Block inter-VM communication (optional)
sudo iptables -I FORWARD -i br0 -o br0 -j DROP
sudo iptables -I FORWARD -i br0 -o br0 -m state --state ESTABLISHED,RELATED -j ACCEPT
```

## Performance Tuning

### CPU Optimization

#### CPU Isolation
```bash
# Isolate CPUs for VM use (GRUB configuration)
GRUB_CMDLINE_LINUX="isolcpus=2-7 nohz_full=2-7 rcu_nocbs=2-7"
```

#### CPU Pinning
```hcl
config {
  # CPU topology configuration
  cmdline = "isolcpus=1-3"
}

resources {
  cpu = 3000  # Use 3 isolated cores
}
```

### Memory Optimization

#### Huge Pages
```bash
# Enable 2MB huge pages
echo 1024 > /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages

# Make persistent
echo 'vm.nr_hugepages=1024' >> /etc/sysctl.conf
```

#### Memory Configuration
```hcl
config {
  # Use huge pages for better performance
  cmdline = "hugepages=512 hugepagesz=2M"
}
```

### Storage Optimization

#### Fast Storage
```bash
# Use NVMe/SSD for images and data
sudo mkdir -p /fast-storage/vm-images
sudo mkdir -p /fast-storage/nomad-data
```

#### Storage Configuration
```hcl
config {
  image_paths = ["/fast-storage/vm-images"]
  data_dir = "/fast-storage/nomad-data"
}
```

### Network Optimization

#### Network Buffer Tuning
```bash
# Increase network buffers
echo 'net.core.rmem_max = 16777216' >> /etc/sysctl.conf
echo 'net.core.wmem_max = 16777216' >> /etc/sysctl.conf
```

#### Bridge Optimization
```bash
# Optimize bridge for VMs
echo 0 > /proc/sys/net/bridge/bridge-nf-call-iptables
echo 0 > /proc/sys/net/bridge/bridge-nf-call-ip6tables
```

This configuration reference covers all aspects of configuring the Nomad Cloud Hypervisor driver for various use cases, from basic VMs to high-performance computing workloads with GPU acceleration.