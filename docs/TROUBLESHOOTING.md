# Troubleshooting Guide

This guide helps diagnose and resolve common issues with the Nomad Cloud Hypervisor driver.

## Table of Contents

- [General Debugging Steps](#general-debugging-steps)
- [Driver Loading Issues](#driver-loading-issues)
- [VM Startup Problems](#vm-startup-problems)
- [Network Connectivity Issues](#network-connectivity-issues)
- [Storage and Disk Issues](#storage-and-disk-issues)
- [Performance Issues](#performance-issues)
- [Device Passthrough Problems](#device-passthrough-problems)
- [Cloud-Init Issues](#cloud-init-issues)
- [Resource Allocation Problems](#resource-allocation-problems)
- [Log Analysis](#log-analysis)
- [Common Error Messages](#common-error-messages)

## General Debugging Steps

### 1. Enable Debug Logging

**Nomad Client Configuration:**
```hcl
log_level = "DEBUG"
enable_debug = true
log_file = "/var/log/nomad/nomad.log"
```

**Environment Variables:**
```bash
export NOMAD_LOG_LEVEL=DEBUG
export RUST_LOG=cloud_hypervisor=debug
```

### 2. Check System Status

```bash
# Check Nomad status
sudo systemctl status nomad

# Check driver status
nomad node status -self | grep -A 10 "Driver Status"

# Verify Cloud Hypervisor installation
cloud-hypervisor --version
ch-remote --version
virtiofsd --version

# Check virtualization support
egrep -o '(vmx|svm)' /proc/cpuinfo
lsmod | grep -E '(kvm_intel|kvm_amd)'
ls -la /dev/kvm
```

### 3. Verify Network Setup

```bash
# Check bridge interface
ip link show br0
ip addr show br0

# Verify bridge is up
bridge link show

# Test bridge connectivity
ping $(ip route | grep br0 | awk '{print $9}' | head -1)
```

## Driver Loading Issues

### Problem: Driver Not Loading

**Symptoms:**
- Driver not visible in `nomad node status -self`
- Error: "unknown driver 'virt'"

**Diagnosis:**
```bash
# Check plugin file
ls -la /opt/nomad/plugins/nomad-driver-ch

# Verify plugin permissions
ldd /opt/nomad/plugins/nomad-driver-ch

# Check Nomad configuration
nomad config validate /etc/nomad.d/client.hcl

# Check Nomad logs
journalctl -u nomad -f | grep -i "nomad-driver-ch"
```

**Solutions:**

1. **Missing Plugin File:**
   ```bash
   # Download or rebuild plugin
   wget https://github.com/ccheshirecat/nomad-driver-ch/releases/latest/download/nomad-driver-ch
   sudo cp nomad-driver-ch /opt/nomad/plugins/
   sudo chmod +x /opt/nomad/plugins/nomad-driver-ch
   ```

2. **Wrong Plugin Directory:**
   ```hcl
   client {
     plugin_dir = "/opt/nomad/plugins"  # Ensure correct path
   }
   ```

3. **Configuration Errors:**
   ```bash
   # Validate configuration syntax
   nomad config validate /etc/nomad.d/client.hcl

   # Check plugin configuration
   nomad agent-info | grep -A 20 "nomad-driver-ch"
   ```

4. **Permission Issues:**
   ```bash
   sudo chown nomad:nomad /opt/nomad/plugins/nomad-driver-ch
   sudo chmod +x /opt/nomad/plugins/nomad-driver-ch
   ```

### Problem: Binary Validation Failed

**Symptoms:**
- Error: "cloud-hypervisor binary not found"
- Error: "virtiofsd binary not found"

**Solutions:**

1. **Install Missing Binaries:**
   ```bash
   # Install Cloud Hypervisor
   wget https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/v48.0/cloud-hypervisor
   sudo mv cloud-hypervisor /usr/local/bin/cloud-hypervisor
   sudo chmod +x /usr/local/bin/cloud-hypervisor

   # Install virtiofsd
   sudo apt-get install virtiofsd  # Ubuntu/Debian
   # OR
   sudo dnf install virtiofsd      # RHEL/Fedora
   ```

2. **Update Binary Paths:**
   ```hcl
   config {
     cloud_hypervisor {
       bin = "/usr/local/bin/cloud-hypervisor"
       virtiofsd_bin = "/usr/libexec/virtiofsd"
     }
   }
   ```

3. **Use Validation Skip Mode for Tests:**
   ```hcl
   config {
     skip_binary_validation = true
   }
   ```
   > **Note:** Skip mode is intended for CI or sandbox environments without virtualization support. Production deployments should keep validation enabled so missing dependencies surface immediately.

4. **Check glibc Compatibility:**
   ```bash
   # Verify host glibc version
   ldd --version

   # Inspect binary requirements
   strings $(which cloud-hypervisor) | grep GLIBC_
   ```
   If the host glibc is older than required (e.g., 2.31 on factory.ai vs. binary built against 2.34), use the statically-linked release (`cloud-hypervisor-static`) or run within a container/VM that provides newer glibc.

## VM Startup Problems

### Problem: VM Fails to Start

**Symptoms:**
- Task stuck in "starting" state
- Error: "Failed to create VM"
- Error: "VM boot failed"

**Diagnosis:**
```bash
# Check task status
nomad alloc status <alloc-id>
nomad alloc logs <alloc-id> <task-name>

# Check Cloud Hypervisor logs
tail -f /var/log/cloud-hypervisor.log

# Check VM serial console
tail -f /opt/nomad/data/alloc/<alloc-id>/<task>/serial.log

# Test Cloud Hypervisor directly
sudo cloud-hypervisor \
  --kernel /boot/vmlinuz \
  --disk path=/var/lib/images/test.img \
  --cpus boot=1 \
  --memory size=512M \
  --net tap=test-tap
```

**Common Solutions:**

1. **Image Format Issues:**
   ```bash
   # Check image format
   qemu-img info /path/to/image.img

   # Convert if necessary
   qemu-img convert -f raw -O qcow2 input.img output.qcow2
   ```

2. **Missing Kernel/Initramfs:**
   ```bash
   # Check kernel files exist
   ls -la /boot/vmlinuz*
   ls -la /boot/initramfs*

   # Update driver configuration
   config {
     cloud_hypervisor {
       default_kernel = "/boot/vmlinuz-5.15.0"
       default_initramfs = "/boot/initramfs-5.15.0.img"
     }
   }
   ```

3. **Insufficient Resources:**
   ```hcl
   resources {
     cpu    = 1000  # At least 1000 (1 core)
     memory = 512   # At least 256MB
   }
   ```

4. **Image Path Not Allowed:**
   ```hcl
   config {
     image_paths = [
       "/var/lib/images",
       "/path/to/your/images"
     ]
   }
   ```

### Problem: "Failed to parse disk image format"

**Cause:** Cloud Hypervisor cannot read the disk image

**Solutions:**

1. **Check Image Integrity:**
   ```bash
   qemu-img check /path/to/image.img
   ```

2. **Verify Image Format:**
   ```bash
   file /path/to/image.img
   qemu-img info /path/to/image.img
   ```

3. **Fix Corrupted Images:**
   ```bash
   qemu-img check -r all /path/to/image.img
   ```

4. **Use Supported Format:**
   ```bash
   # Convert to supported format
   qemu-img convert -f qcow2 -O raw input.qcow2 output.img
   ```

## Network Connectivity Issues

### Problem: VM Has No Network Access

**Symptoms:**
- VM cannot reach internet
- Cannot SSH to VM
- Network interface not visible in VM

**Diagnosis:**
```bash
# Check bridge configuration
ip link show br0
ip addr show br0
bridge link show

# Check TAP interfaces
ip link show | grep tap

# Check iptables rules
sudo iptables -L -v -n | grep br0

# Test bridge connectivity
ping <bridge-gateway-ip>

# Check VM network inside VM
# (SSH to VM first)
ip addr show
ip route show
cat /etc/resolv.conf
ping 8.8.8.8
```

**Solutions:**

1. **Bridge Not Configured:**
   ```bash
   # Create and configure bridge
   sudo ip link add br0 type bridge
   sudo ip addr add 192.168.1.1/24 dev br0
   sudo ip link set br0 up

   # Enable IP forwarding
   echo 1 | sudo tee /proc/sys/net/ipv4/ip_forward
   echo 'net.ipv4.ip_forward=1' | sudo tee -a /etc/sysctl.conf
   ```

2. **TAP Interface Issues:**
   ```bash
   # Check TAP creation
   sudo ip tuntap add dev tap-test mode tap
   sudo ip link set tap-test master br0
   sudo ip link set tap-test up

   # Clean up test
   sudo ip link delete tap-test
   ```

3. **Firewall Blocking Traffic:**
   ```bash
   # Allow bridge traffic
   sudo iptables -A FORWARD -i br0 -j ACCEPT
   sudo iptables -A FORWARD -o br0 -j ACCEPT

   # Save rules
   sudo iptables-save > /etc/iptables/rules.v4
   ```

4. **Cloud-Init Network Config:**
   ```bash
   # Check cloud-init network config (inside VM)
   sudo cat /etc/netplan/50-cloud-init.yaml
   sudo netplan apply

   # Check cloud-init logs
   sudo tail -f /var/log/cloud-init.log
   ```

### Problem: Cannot Access VM Services

**Symptoms:**
- Services running but not accessible from host
- Port mapping not working

**Solutions:**

1. **Check Port Configuration:**
   ```hcl
   network {
     port "web" {
       static = 8080
       to     = 80
     }
   }

   config {
     network_interface {
       bridge {
         name = "br0"
         ports = ["web"]
       }
     }
   }
   ```

2. **Verify Service Binding:**
   ```bash
   # Inside VM - check service binding
   sudo netstat -tlnp | grep :80
   sudo ss -tlnp | grep :80

   # Ensure service binds to 0.0.0.0, not 127.0.0.1
   ```

3. **Test Connectivity:**
   ```bash
   # From host
   telnet <vm-ip> 80
   nmap -p 80 <vm-ip>

   # Check Nomad port allocation
   nomad alloc status <alloc-id>
   ```

## Storage and Disk Issues

### Problem: Disk Space Issues

**Symptoms:**
- VM startup fails with disk space error
- VM runs out of space during operation

**Solutions:**

1. **Check Available Space:**
   ```bash
   df -h /var/lib/images
   df -h /opt/nomad/data
   ```

2. **Increase Disk Size:**
   ```hcl
   config {
     primary_disk_size = 20480  # 20GB
   }
   ```

3. **Use Thin Provisioning:**
   ```hcl
   config {
     use_thin_copy = true
     primary_disk_size = 10240
   }
   ```

4. **Clean Up Old Data:**
   ```bash
   # Clean up old allocations
   sudo find /opt/nomad/data/alloc -name "*.img" -mtime +7 -delete

   # Clean up garbage collected allocations
   nomad system gc
   ```

### Problem: VirtioFS Mount Failures

**Symptoms:**
- Shared directories not accessible in VM
- Mount errors in VM logs

**Diagnosis:**
```bash
# Check virtiofsd processes
ps aux | grep virtiofsd

# Check mount points in VM
mount | grep virtiofs
df -h | grep virtiofs

# Check Nomad volume configuration
nomad alloc status <alloc-id>
```

**Solutions:**

1. **Verify Volume Configuration:**
   ```hcl
   # Host volume configuration
   client {
     host_volume "data" {
       path = "/opt/app-data"
       read_only = false
     }
   }

   # Task volume mount
   volume_mount {
     volume      = "data"
     destination = "/app/data"
     read_only   = false
   }
   ```

2. **Check Directory Permissions:**
   ```bash
   sudo chown -R nomad:nomad /opt/app-data
   sudo chmod -R 755 /opt/app-data
   ```

3. **Restart virtiofsd:**
   ```bash
   # Kill existing virtiofsd processes
   sudo pkill virtiofsd

   # Restart task to recreate mounts
   nomad alloc restart <alloc-id>
   ```

## Performance Issues

### Problem: Poor VM Performance

**Symptoms:**
- Slow VM startup
- High CPU usage on host
- Poor I/O performance

**Solutions:**

1. **CPU Optimization:**
   ```bash
   # Check CPU usage
   top -p $(pgrep cloud-hypervisor)

   # Enable CPU isolation
   # Edit /etc/default/grub
   GRUB_CMDLINE_LINUX="isolcpus=2-7 nohz_full=2-7"
   sudo update-grub && sudo reboot
   ```

2. **Memory Optimization:**
   ```bash
   # Enable huge pages
   echo 1024 | sudo tee /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages

   # Make persistent
   echo 'vm.nr_hugepages=1024' | sudo tee -a /etc/sysctl.conf
   ```

3. **Storage Optimization:**
   ```bash
   # Use faster storage
   sudo mkdir -p /nvme/vm-images
   sudo mkdir -p /nvme/nomad-data

   # Update configuration
   config {
     image_paths = ["/nvme/vm-images"]
     data_dir = "/nvme/nomad-data"
   }
   ```

4. **Network Optimization:**
   ```bash
   # Optimize network buffers
   echo 'net.core.rmem_max = 16777216' | sudo tee -a /etc/sysctl.conf
   echo 'net.core.wmem_max = 16777216' | sudo tee -a /etc/sysctl.conf
   sudo sysctl -p
   ```

### Problem: High Memory Usage

**Solutions:**

1. **Monitor Memory Usage:**
   ```bash
   # Check host memory
   free -h

   # Check VM memory allocation
   nomad node status -stats
   ```

2. **Optimize VM Memory:**
   ```hcl
   resources {
     memory = 1024  # Reduce if possible
   }

   config {
     # Use memory ballooning if available
     cmdline = "console=ttyS0 mem=1024M"
   }
   ```

## Device Passthrough Problems

### Problem: GPU Passthrough Not Working

**Symptoms:**
- GPU not visible in VM
- VFIO binding errors
- VM fails to start with GPU config

**Diagnosis:**
```bash
# Check IOMMU status
dmesg | grep -i iommu

# Check VFIO binding
lspci -nnk -d 10de:2204

# Check IOMMU groups
find /sys/kernel/iommu_groups/ -name "devices" -exec ls -la {} \;
```

**Solutions:**

1. **Enable IOMMU:**
   ```bash
   # Edit /etc/default/grub
   GRUB_CMDLINE_LINUX="intel_iommu=on iommu=pt vfio-pci.ids=10de:2204"
   sudo update-grub
   sudo reboot
   ```

2. **Bind Device to VFIO:**
   ```bash
   # Bind GPU to VFIO driver
   echo "10de 2204" | sudo tee /sys/bus/pci/drivers/vfio-pci/new_id
   echo "0000:01:00.0" | sudo tee /sys/bus/pci/drivers/nvidia/unbind
   echo "0000:01:00.0" | sudo tee /sys/bus/pci/drivers/vfio-pci/bind
   ```

3. **Update Driver Configuration:**
   ```hcl
   config {
     vfio {
       allowlist = ["10de:2204", "10de:1aef"]
     }
   }
   ```

4. **Check VM Configuration:**
   ```hcl
   config {
     vfio_devices = ["10de:2204"]
   }

   resources {
     device "nvidia/gpu" {
       count = 1
     }
   }
   ```

## Cloud-Init Issues

### Problem: Cloud-Init Not Working

**Symptoms:**
- Commands not executing
- Users not created
- Network not configured

**Diagnosis:**
```bash
# Check cloud-init status (inside VM)
sudo cloud-init status --wait
sudo cloud-init analyze show

# Check cloud-init logs
sudo cat /var/log/cloud-init.log
sudo cat /var/log/cloud-init-output.log

# Verify cloud-init data source
sudo cat /var/lib/cloud/data/instance-id
```

**Solutions:**

1. **Check ISO Creation:**
   ```bash
   # Verify ISO exists
   ls -la /opt/nomad/data/alloc/<alloc-id>/<task>/*.iso

   # Check ISO contents
   sudo mkdir -p /tmp/ci-mount
   sudo mount -o loop /path/to/cloud-init.iso /tmp/ci-mount
   ls -la /tmp/ci-mount/
   sudo umount /tmp/ci-mount
   ```

2. **Validate User Data:**
   ```bash
   # Check user data syntax
   cloud-init devel schema --config-file user-data.yml
   ```

3. **Debug Inside VM:**
   ```bash
   # Re-run cloud-init modules
   sudo cloud-init clean
   sudo cloud-init init
   sudo cloud-init modules --mode=config
   sudo cloud-init modules --mode=final
   ```

4. **Check Network Timing:**
   ```hcl
   config {
     # Add delay for network setup
     user_data = <<EOF
   #cloud-config
   bootcmd:
     - sleep 10  # Wait for network
   runcmd:
     - systemctl restart networking
     - dhclient eth0
   EOF
   }
   ```

## Resource Allocation Problems

### Problem: Tasks Not Scheduling

**Symptoms:**
- Tasks stuck in "pending" state
- Error: "no nodes available for placement"

**Solutions:**

1. **Check Node Resources:**
   ```bash
   nomad node status -verbose <node-id>
   nomad node status -stats <node-id>
   ```

2. **Review Resource Requirements:**
   ```hcl
   resources {
     cpu    = 1000  # Reduce if too high
     memory = 512   # Reduce if too high
   }
   ```

3. **Check Constraints:**
   ```hcl
   # Remove restrictive constraints
   constraint {
     attribute = "${node.class}"
     value     = "compute"  # Ensure nodes have this class
   }
   ```

4. **Increase Node Capacity:**
   ```bash
   # Add more nodes or increase resources
   nomad node status
   ```

## Log Analysis

### Key Log Files

1. **Nomad Logs:**
   ```bash
   journalctl -u nomad -f
   tail -f /var/log/nomad/nomad.log
   ```

2. **Cloud Hypervisor Logs:**
   ```bash
   tail -f /var/log/cloud-hypervisor.log
   ```

3. **VM Console Logs:**
   ```bash
   tail -f /opt/nomad/data/alloc/<alloc-id>/<task>/serial.log
   ```

4. **Cloud-Init Logs (inside VM):**
   ```bash
   sudo tail -f /var/log/cloud-init.log
   sudo tail -f /var/log/cloud-init-output.log
   ```

### Log Analysis Commands

```bash
# Search for errors
grep -i error /var/log/nomad/nomad.log
journalctl -u nomad | grep -i "failed\|error"

# Follow specific allocation
nomad alloc logs -f <alloc-id> <task-name>

# Check driver-specific logs
journalctl -u nomad | grep "nomad-driver-ch"

# Search for network issues
journalctl | grep -i "bridge\|tap\|network"
```

## Common Error Messages

### "cloud-hypervisor binary validation failed"
**Cause:** Cloud Hypervisor binary not found or not executable
**Solution:** Install Cloud Hypervisor and update configuration paths

### "Failed to create tap interface"
**Cause:** Permission issues or network configuration
**Solution:** Check bridge configuration and permissions

### "No nodes available for placement"
**Cause:** Resource constraints or node eligibility
**Solution:** Check resource requirements and node capacity

### "Image path not in allowed paths"
**Cause:** Security restriction on image locations
**Solution:** Update `image_paths` configuration

### "Failed to bind VFIO device"
**Cause:** IOMMU not enabled or device already in use
**Solution:** Enable IOMMU and bind device to VFIO driver

### "Cloud-init timeout"
**Cause:** Cloud-init taking too long or failing
**Solution:** Check cloud-init logs and network timing

### "VM boot failed with status 500"
**Cause:** Cloud Hypervisor API error
**Solution:** Check kernel, initramfs, and image configuration

## Getting Help

### Collecting Debug Information

When reporting issues, collect this information:

```bash
# System information
uname -a
lscpu | grep -i virtualization
free -h
df -h

# Nomad information
nomad version
nomad node status -self
nomad agent-info

# Cloud Hypervisor information
cloud-hypervisor --version
lsmod | grep kvm

# Network information
ip link show
bridge link show
iptables -L -v -n

# Logs
journalctl -u nomad --since "1 hour ago" > nomad-logs.txt
nomad alloc logs <alloc-id> <task-name> > task-logs.txt
```

### Community Support

- **GitHub Issues**: [Report bugs and issues](https://github.com/ccheshirecat/nomad-driver-ch/issues)
- **GitHub Discussions**: [Ask questions and share experiences](https://github.com/ccheshirecat/nomad-driver-ch/discussions)
- **Documentation**: [Check latest documentation](https://github.com/ccheshirecat/nomad-driver-ch/tree/main/docs)

When reporting issues:
1. Include your Nomad and driver versions
2. Provide your configuration files (redact sensitive data)
3. Include relevant log output
4. Describe expected vs actual behavior
5. List steps to reproduce the issue