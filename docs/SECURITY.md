# Security Guide

This guide covers security considerations and best practices for the Nomad Cloud Hypervisor Driver.

## Overview

The Cloud Hypervisor Driver provides several security features to ensure safe VM orchestration:

- **Resource Isolation**: VMs are isolated from each other and the host
- **Image Path Restrictions**: Only allowed paths can be used for VM images
- **Network Isolation**: Configurable network access controls
- **Seccomp Filtering**: Optional system call filtering

## Configuration Security

### Image Path Security

Always restrict image paths to prevent unauthorized access:

```hcl
plugin "nomad-driver-ch" {
  config {
    image_paths = [
      "/var/lib/images",
      "/opt/vm-images"
    ]
  }
}
```

### Network Security

Configure network isolation appropriately:

```hcl
plugin "nomad-driver-ch" {
  config {
    network {
      bridge = "br0"
      subnet_cidr = "192.168.1.0/24"
      gateway = "192.168.1.1"
      ip_pool_start = "192.168.1.100"
      ip_pool_end = "192.168.1.200"
    }
  }
}
```

## Host Security

### Kernel Requirements

Ensure your host kernel has the following security features enabled:

```bash
# Check if security modules are loaded
lsmod | grep -E "(apparmor|selinux|smack)"

# Verify IOMMU support for VFIO
dmesg | grep -i iommu
```

### VFIO Security

When using VFIO device passthrough:

1. **IOMMU**: Ensure IOMMU is enabled in the kernel
2. **Device Isolation**: Only pass through devices that need direct access
3. **Group Isolation**: Pass through entire IOMMU groups, not individual devices

```bash
# Enable IOMMU in GRUB
echo "GRUB_CMDLINE_LINUX=\"intel_iommu=on iommu=pt\"" >> /etc/default/grub
update-grub

# Bind device to VFIO
echo "10de 2204" > /sys/bus/pci/drivers/vfio-pci/new_id
echo "0000:01:00.0" > /sys/bus/pci/drivers/vfio-pci/bind
```

## VM Security

### Resource Limits

Always set appropriate resource limits:

```hcl
resources {
  cpu    = 2000  # 2 CPU cores
  memory = 2048  # 2GB RAM
}
```

### Seccomp Filtering

Enable seccomp filtering for additional security:

```hcl
plugin "nomad-driver-ch" {
  config {
    cloud_hypervisor {
      seccomp = "true"
    }
  }
}
```

## Network Security

### Firewall Configuration

Configure host firewall rules:

```bash
# Allow Nomad server communication
ufw allow 4646/tcp
ufw allow 4647/tcp
ufw allow 4648/tcp

# Allow bridge traffic
ufw allow in on br0
ufw allow out on br0
```

### iptables Rules

The driver automatically manages iptables rules for port forwarding. Monitor these rules:

```bash
# View current NAT rules
iptables -t nat -L NOMAD_CH_PRT

# View current filter rules
iptables -t filter -L NOMAD_CH_FW
```

## Access Control

### Nomad ACLs

Use Nomad's ACL system to control access:

```hcl
acl {
  enabled = true
}

# Example policy
policy "vm-operator" {
  description = "VM operator policy"

  rule {
    operation = "read"
    resource  = "node"
  }

  rule {
    operation = "write"
    resource  = "driver"
  }
}
```

## Monitoring and Logging

### Security Monitoring

Monitor for security events:

```bash
# Monitor authentication failures
tail -f /var/log/auth.log | grep -i failed

# Monitor unusual network activity
tail -f /var/log/syslog | grep -i "nomad\|cloud-hypervisor"
```

### Audit Logging

Enable audit logging for compliance:

```hcl
audit {
  enabled = true
  sink "file" {
    type = "file"
    path = "/var/log/nomad_audit.log"
  }
}
```

## Best Practices

### Image Security

1. **Scan Images**: Regularly scan VM images for vulnerabilities
2. **Update Images**: Keep base images updated with security patches
3. **Minimal Images**: Use minimal base images to reduce attack surface

### Network Security

1. **Segmentation**: Use different networks for different security zones
2. **Access Control**: Implement proper firewall rules
3. **Monitoring**: Monitor network traffic for anomalies

### Resource Management

1. **Limits**: Always set resource limits for tasks
2. **Monitoring**: Monitor resource usage patterns
3. **Quotas**: Use Nomad quotas to prevent resource exhaustion

## Incident Response

### Security Incidents

If you suspect a security incident:

1. **Isolate**: Isolate affected VMs immediately
2. **Investigate**: Check logs and monitoring data
3. **Report**: Report incidents according to your security policy
4. **Remediate**: Apply fixes and update configurations

### Recovery Procedures

```bash
# Stop all VMs
nomad job stop -purge <job-name>

# Clean up resources
nomad system gc

# Restart with updated configuration
nomad job run <job-file>
```

## Compliance

### Security Standards

The driver can help meet various compliance requirements:

- **PCI DSS**: Network segmentation and access controls
- **HIPAA**: Data isolation and audit logging
- **GDPR**: Data protection and privacy controls

### Security Assessments

Regularly perform security assessments:

1. **Vulnerability Scanning**: Scan hosts and VMs regularly
2. **Penetration Testing**: Test security controls periodically
3. **Configuration Reviews**: Review security configurations
4. **Log Analysis**: Analyze logs for security events

## Reporting Security Issues

If you discover a security vulnerability:

1. **Do not** publicly disclose the issue
2. **Report** to the security team
3. **Provide** detailed information about the vulnerability
4. **Allow time** for the team to investigate and fix

## Additional Resources

- [Nomad Security Documentation](https://www.nomadproject.io/docs/internals/security)
- [Cloud Hypervisor Security](https://github.com/cloud-hypervisor/cloud-hypervisor/security)
- [Linux Kernel Security](https://www.kernel.org/doc/html/latest/security/)