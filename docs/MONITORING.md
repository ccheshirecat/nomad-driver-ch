# Monitoring Guide

This guide covers monitoring and observability for the Nomad Cloud Hypervisor Driver.

## Resource Statistics

The driver provides real-time VM resource statistics that can be accessed through Nomad's monitoring capabilities.

### Viewing Allocation Statistics

```bash
# View allocation statistics
nomad alloc status <alloc-id>

# Monitor resource usage in real-time
nomad alloc logs -f <alloc-id> <task-name>
```

### Resource Metrics

The driver exposes the following metrics:

- **CPU Usage**: CPU time consumed by the VM
- **Memory Usage**: Memory allocated to the VM
- **Network I/O**: Network traffic statistics
- **Disk I/O**: Disk read/write statistics

## VM Health Checks

Configure health checks for VM services:

```hcl
task "web-server" {
  driver = "ch"

  config {
    image = "/var/lib/images/nginx.img"
    network_interface {
      bridge {
        name = "br0"
        static_ip = "192.168.1.100"
      }
    }
  }

  service {
    name = "web"
    port = "http"

    check {
      type     = "http"
      path     = "/"
      interval = "30s"
      timeout  = "5s"
      address_mode = "alloc"
    }
  }
}
```

## Logging

### Driver Logs

The driver logs are available through Nomad's logging system:

```bash
# View driver logs
nomad alloc logs <alloc-id> <task-name>

# Follow logs in real-time
nomad alloc logs -f <alloc-id> <task-name>
```

### Cloud Hypervisor Logs

Cloud Hypervisor logs can be configured in the driver configuration:

```hcl
plugin "nomad-driver-ch" {
  config {
    cloud_hypervisor {
      log_file = "/var/log/cloud-hypervisor.log"
    }
  }
}
```

## Performance Monitoring

### Benchmarking VM Performance

```bash
# CPU performance test
stress-ng --cpu 4 --timeout 60s

# Memory performance test
stress-ng --vm 2 --vm-bytes 1G --timeout 60s

# Network performance test
iperf3 -c <server-ip> -t 30
```

### Resource Limits

Monitor resource usage to ensure VMs stay within allocated limits:

```bash
# Check CPU usage
top -p <vm-pid>

# Check memory usage
free -h

# Check disk I/O
iostat -x 1
```

## Troubleshooting

### Common Issues

#### High CPU Usage

**Symptoms:**
- VM consuming excessive CPU resources
- Poor performance of other tasks

**Solutions:**
1. Check for CPU-intensive processes in the VM
2. Adjust CPU allocation in the job specification
3. Monitor for memory pressure causing excessive swapping

#### Memory Issues

**Symptoms:**
- VM being killed by OOM killer
- Poor performance due to swapping

**Solutions:**
1. Increase memory allocation
2. Monitor memory usage patterns
3. Check for memory leaks in applications

#### Network Connectivity Issues

**Symptoms:**
- VM cannot reach external networks
- Services not accessible from outside

**Solutions:**
1. Verify bridge configuration
2. Check iptables rules
3. Validate IP allocation

### Debug Mode

Enable debug logging for detailed troubleshooting:

```hcl
# In Nomad client configuration
log_level = "DEBUG"
enable_debug = true
```

### VM State Inspection

```bash
# Check Cloud Hypervisor processes
ps aux | grep cloud-hypervisor

# Inspect VM via ch-remote
ch-remote --api-socket /path/to/api.sock info

# Monitor VM console output
tail -f /opt/nomad/data/alloc/<alloc-id>/<task>/serial.log
```

## Metrics Collection

### Prometheus Integration

The driver can be integrated with Prometheus for metrics collection:

```hcl
# Example Prometheus configuration
scrape_configs:
  - job_name: 'nomad-driver-ch'
    static_configs:
      - targets: ['localhost:4646']
    metrics_path: '/v1/metrics'
    params:
      format: ['prometheus']
```

### Grafana Dashboards

Create dashboards to visualize:
- VM resource utilization over time
- Network I/O patterns
- Health check success rates
- Task allocation success/failure rates

## Alerting

Set up alerts for:
- VM resource usage above thresholds
- Health check failures
- Task allocation failures
- Network connectivity issues

## Security Monitoring

Monitor for:
- Unauthorized access attempts
- Unusual network traffic patterns
- Resource usage anomalies
- Security policy violations