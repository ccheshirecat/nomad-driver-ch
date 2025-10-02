# Task Examples

This document provides comprehensive examples of using the Nomad Cloud Hypervisor driver for various use cases.

## Table of Contents

- [Basic Examples](#basic-examples)
- [Web Services](#web-services)
- [Database Workloads](#database-workloads)
- [Machine Learning and AI](#machine-learning-and-ai)
- [Development Environments](#development-environments)
- [Batch Processing](#batch-processing)
- [Multi-tier Applications](#multi-tier-applications)
- [Advanced Configurations](#advanced-configurations)

## Basic Examples

### Simple Alpine VM

```hcl
job "simple-alpine" {
  datacenters = ["dc1"]
  type = "batch"

  group "test" {
    task "alpine" {
      driver = "virt"

      config {
        image = "/var/lib/images/alpine-3.18.img"
        hostname = "test-alpine"

        default_user_password = "alpine123"

        cmds = [
          "echo 'Hello from Cloud Hypervisor VM!'",
          "uname -a",
          "free -m",
          "df -h"
        ]
      }

      resources {
        cpu    = 500   # 0.5 CPU core
        memory = 256   # 256MB RAM
      }
    }
  }
}
```

### Ubuntu with SSH Access

```hcl
job "ubuntu-ssh" {
  datacenters = ["dc1"]
  type = "service"

  group "vm" {
    task "ubuntu" {
      driver = "virt"

      config {
        image = "/var/lib/images/ubuntu-22.04.img"
        hostname = "ubuntu-server"

        # Enable SSH access
        default_user_authorized_ssh_key = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC..."

        # Network configuration with static IP
        network_interface {
          bridge {
            name = "br0"
            static_ip = "192.168.1.150"
            ports = ["ssh"]
          }
        }

        cmds = [
          "systemctl enable ssh",
          "systemctl start ssh"
        ]
      }

      resources {
        cpu    = 1000
        memory = 1024
      }
    }

    network {
      port "ssh" {
        static = 22
        to     = 22
      }
    }

    service {
      name = "ubuntu-ssh"
      port = "ssh"
      tags = ["ssh", "ubuntu"]

      check {
        type     = "tcp"
        interval = "30s"
        timeout  = "5s"
      }
    }
  }
}
```

## Web Services

### Nginx Web Server

```hcl
job "nginx-server" {
  datacenters = ["dc1"]
  type = "service"

  group "web" {
    count = 2

    task "nginx" {
      driver = "virt"

      config {
        image = "/var/lib/images/nginx-alpine.img"
        hostname = "nginx-${NOMAD_ALLOC_INDEX}"

        # Custom cloud-init configuration
        user_data = <<EOF
#cloud-config
packages:
  - nginx
  - curl

write_files:
  - path: /etc/nginx/nginx.conf
    content: |
      events {
        worker_connections 1024;
      }
      http {
        server {
          listen 80;
          location / {
            root /var/www/html;
            index index.html;
          }
          location /health {
            return 200 "healthy\n";
            add_header Content-Type text/plain;
          }
        }
      }
    permissions: '0644'

  - path: /var/www/html/index.html
    content: |
      <!DOCTYPE html>
      <html>
        <head><title>Nomad Cloud Hypervisor</title></head>
        <body>
          <h1>Hello from VM ${HOSTNAME}!</h1>
          <p>Deployed via Nomad Cloud Hypervisor driver</p>
        </body>
      </html>
    permissions: '0644'

runcmd:
  - mkdir -p /var/www/html
  - nginx -g 'daemon off;' &
EOF

        network_interface {
          bridge {
            name = "br0"
            ports = ["http"]
          }
        }
      }

      resources {
        cpu    = 1000
        memory = 512
      }
    }

    network {
      port "http" {
        to = 80
      }
    }

    service {
      name = "nginx"
      port = "http"
      tags = ["web", "nginx", "frontend"]

      check {
        type     = "http"
        path     = "/health"
        interval = "10s"
        timeout  = "3s"
      }
    }
  }
}
```

### Node.js Application

```hcl
job "nodejs-app" {
  datacenters = ["dc1"]
  type = "service"

  group "app" {
    task "nodejs" {
      driver = "virt"

      template {
        data = <<EOF
const express = require('express');
const app = express();
const port = 3000;

app.get('/', (req, res) => {
  res.json({
    message: 'Hello from Node.js VM!',
    hostname: process.env.HOSTNAME,
    timestamp: new Date().toISOString()
  });
});

app.get('/health', (req, res) => {
  res.json({ status: 'healthy' });
});

app.listen(port, '0.0.0.0', () => {
  console.log(`App running on port ${port}`);
});
EOF
        destination = "local/app.js"
        perms       = "644"
      }

      config {
        image = "/var/lib/images/node-18-alpine.img"
        hostname = "nodejs-app"

        user_data = <<EOF
#cloud-config
packages:
  - nodejs
  - npm

write_files:
  - path: /app/package.json
    content: |
      {
        "name": "nomad-vm-app",
        "version": "1.0.0",
        "dependencies": {
          "express": "^4.18.0"
        },
        "scripts": {
          "start": "node app.js"
        }
      }
    permissions: '0644'

runcmd:
  - mkdir -p /app
  - cp /alloc/data/app.js /app/
  - cd /app && npm install
  - cd /app && npm start
EOF

        network_interface {
          bridge {
            name = "br0"
            ports = ["api"]
          }
        }
      }

      resources {
        cpu    = 1000
        memory = 768
      }
    }

    network {
      port "api" {
        to = 3000
      }
    }

    service {
      name = "nodejs-api"
      port = "api"
      tags = ["api", "nodejs"]

      check {
        type     = "http"
        path     = "/health"
        interval = "15s"
        timeout  = "5s"
      }
    }
  }
}
```

## Database Workloads

### PostgreSQL Database

```hcl
job "postgres-db" {
  datacenters = ["dc1"]
  type = "service"

  group "db" {
    task "postgres" {
      driver = "virt"

      config {
        image = "/var/lib/images/postgres-14.img"
        hostname = "postgres-primary"

        # Use thin copy for better performance
        use_thin_copy = true
        primary_disk_size = 20480  # 20GB

        # Static IP for consistent access
        network_interface {
          bridge {
            name = "br0"
            static_ip = "192.168.1.50"
            ports = ["db"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - postgresql
  - postgresql-contrib

write_files:
  - path: /etc/postgresql/postgresql.conf
    content: |
      listen_addresses = '*'
      port = 5432
      max_connections = 100
      shared_buffers = 128MB
      effective_cache_size = 512MB
      maintenance_work_mem = 64MB
      checkpoint_completion_target = 0.9
      wal_buffers = 16MB
      default_statistics_target = 100
      random_page_cost = 1.1
      effective_io_concurrency = 200
      log_destination = 'stderr'
      logging_collector = on
      log_directory = '/var/log/postgresql'
      log_filename = 'postgresql-%Y-%m-%d_%H%M%S.log'
      log_statement = 'all'
    permissions: '0644'

  - path: /etc/postgresql/pg_hba.conf
    content: |
      local   all             postgres                                peer
      local   all             all                                     peer
      host    all             all             192.168.1.0/24          md5
      host    all             all             127.0.0.1/32            md5
      host    all             all             ::1/128                 md5
    permissions: '0644'

  - path: /tmp/init-db.sql
    content: |
      CREATE DATABASE myapp;
      CREATE USER myapp_user WITH ENCRYPTED PASSWORD 'secure_password';
      GRANT ALL PRIVILEGES ON DATABASE myapp TO myapp_user;
    permissions: '0644'

runcmd:
  - systemctl enable postgresql
  - systemctl start postgresql
  - sudo -u postgres psql -f /tmp/init-db.sql
  - systemctl restart postgresql
EOF
      }

      resources {
        cpu    = 2000
        memory = 2048
      }

      # Persistent storage for database
      volume_mount {
        volume      = "postgres-data"
        destination = "/var/lib/postgresql/data"
        read_only   = false
      }
    }

    network {
      port "db" {
        static = 5432
        to     = 5432
      }
    }

    service {
      name = "postgres"
      port = "db"
      tags = ["db", "postgres", "primary"]

      check {
        type     = "tcp"
        interval = "30s"
        timeout  = "5s"
      }
    }
  }

  volume "postgres-data" {
    type      = "host"
    source    = "postgres-data"
    read_only = false
  }
}
```

### Redis Cache

```hcl
job "redis-cache" {
  datacenters = ["dc1"]
  type = "service"

  group "cache" {
    task "redis" {
      driver = "virt"

      config {
        image = "/var/lib/images/redis-alpine.img"
        hostname = "redis-cache"

        network_interface {
          bridge {
            name = "br0"
            static_ip = "192.168.1.60"
            ports = ["redis"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - redis

write_files:
  - path: /etc/redis/redis.conf
    content: |
      bind 0.0.0.0
      port 6379
      protected-mode yes
      requirepass redis_secure_password
      maxmemory 256mb
      maxmemory-policy allkeys-lru
      save 900 1
      save 300 10
      save 60 10000
      dir /var/lib/redis
      logfile /var/log/redis/redis.log
      loglevel notice
    permissions: '0644'

runcmd:
  - mkdir -p /var/lib/redis /var/log/redis
  - chown redis:redis /var/lib/redis /var/log/redis
  - systemctl enable redis
  - systemctl start redis
EOF
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }

    network {
      port "redis" {
        static = 6379
        to     = 6379
      }
    }

    service {
      name = "redis"
      port = "redis"
      tags = ["cache", "redis", "kv-store"]

      check {
        type     = "tcp"
        interval = "10s"
        timeout  = "3s"
      }
    }
  }
}
```

## Machine Learning and AI

### TensorFlow Training Job

```hcl
job "tensorflow-training" {
  datacenters = ["dc1"]
  type = "batch"

  constraint {
    attribute = "${node.class}"
    value     = "gpu"
  }

  group "training" {
    task "train-model" {
      driver = "virt"

      config {
        image = "/var/lib/images/tensorflow-gpu.img"
        hostname = "tf-trainer"

        # GPU passthrough
        vfio_devices = ["10de:2204"]  # NVIDIA RTX 3080

        user_data = <<EOF
#cloud-config
packages:
  - python3
  - python3-pip
  - nvidia-driver-470
  - nvidia-utils-470

write_files:
  - path: /app/train.py
    content: |
      import tensorflow as tf
      import numpy as np
      import os

      print("TensorFlow version:", tf.__version__)
      print("GPU Available: ", tf.config.list_physical_devices('GPU'))

      # Simple model training example
      mnist = tf.keras.datasets.mnist
      (x_train, y_train), (x_test, y_test) = mnist.load_data()
      x_train, x_test = x_train / 255.0, x_test / 255.0

      model = tf.keras.models.Sequential([
        tf.keras.layers.Flatten(input_shape=(28, 28)),
        tf.keras.layers.Dense(128, activation='relu'),
        tf.keras.layers.Dropout(0.2),
        tf.keras.layers.Dense(10)
      ])

      model.compile(optimizer='adam',
                   loss=tf.keras.losses.SparseCategoricalCrossentropy(from_logits=True),
                   metrics=['accuracy'])

      model.fit(x_train, y_train, epochs=5, validation_data=(x_test, y_test))
      model.save('/alloc/data/trained_model')
      print("Training complete! Model saved.")
    permissions: '0755'

runcmd:
  - mkdir -p /app
  - pip3 install tensorflow-gpu numpy
  - cd /app && python3 train.py
EOF
      }

      resources {
        cpu    = 4000
        memory = 8192
        device "nvidia/gpu" {
          count = 1
        }
      }

      # Mount for saving trained models
      volume_mount {
        volume      = "model-output"
        destination = "/alloc/data"
        read_only   = false
      }
    }
  }

  volume "model-output" {
    type      = "host"
    source    = "ml-models"
    read_only = false
  }
}
```

### Jupyter Notebook Server

```hcl
job "jupyter-server" {
  datacenters = ["dc1"]
  type = "service"

  group "notebook" {
    task "jupyter" {
      driver = "virt"

      config {
        image = "/var/lib/images/jupyter-scipy.img"
        hostname = "jupyter-server"

        network_interface {
          bridge {
            name = "br0"
            static_ip = "192.168.1.80"
            ports = ["jupyter"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - python3
  - python3-pip
  - git

write_files:
  - path: /app/jupyter_config.py
    content: |
      c.NotebookApp.ip = '0.0.0.0'
      c.NotebookApp.port = 8888
      c.NotebookApp.open_browser = False
      c.NotebookApp.token = 'nomad-jupyter-token'
      c.NotebookApp.notebook_dir = '/notebooks'
      c.NotebookApp.allow_root = True
    permissions: '0644'

runcmd:
  - mkdir -p /notebooks /app
  - pip3 install jupyter numpy pandas matplotlib scikit-learn
  - jupyter notebook --config=/app/jupyter_config.py &
EOF
      }

      resources {
        cpu    = 2000
        memory = 4096
      }

      # Mount for notebook storage
      volume_mount {
        volume      = "notebooks"
        destination = "/notebooks"
        read_only   = false
      }
    }

    network {
      port "jupyter" {
        to = 8888
      }
    }

    service {
      name = "jupyter"
      port = "jupyter"
      tags = ["jupyter", "notebook", "ml", "data-science"]

      check {
        type     = "http"
        path     = "/tree"
        interval = "30s"
        timeout  = "10s"
      }
    }
  }

  volume "notebooks" {
    type      = "host"
    source    = "jupyter-notebooks"
    read_only = false
  }
}
```

## Development Environments

### Full Development VM

```hcl
job "dev-environment" {
  datacenters = ["dc1"]
  type = "service"

  group "dev" {
    task "devbox" {
      driver = "virt"

      config {
        image = "/var/lib/images/ubuntu-dev.img"
        hostname = "dev-${NOMAD_ALLOC_INDEX}"

        # Larger disk for development tools
        primary_disk_size = 40960  # 40GB

        network_interface {
          bridge {
            name = "br0"
            ports = ["ssh", "web", "api"]
          }
        }

        default_user_authorized_ssh_key = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC..."

        user_data = <<EOF
#cloud-config
packages:
  - git
  - vim
  - curl
  - wget
  - build-essential
  - nodejs
  - npm
  - python3
  - python3-pip
  - docker.io
  - docker-compose

users:
  - name: developer
    groups: sudo, docker
    shell: /bin/bash
    ssh_authorized_keys:
      - ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC...

write_files:
  - path: /home/developer/.vimrc
    content: |
      syntax on
      set number
      set tabstop=2
      set shiftwidth=2
      set expandtab
      set autoindent
    owner: developer:developer
    permissions: '0644'

  - path: /home/developer/.bashrc
    content: |
      export PS1='\u@\h:\w$ '
      alias ll='ls -la'
      alias la='ls -A'
      alias l='ls -CF'
      export PATH=$PATH:/usr/local/go/bin
    owner: developer:developer
    permissions: '0644'

runcmd:
  - systemctl enable ssh
  - systemctl start ssh
  - systemctl enable docker
  - systemctl start docker
  - usermod -aG docker developer
  # Install Go
  - wget -O /tmp/go.tar.gz https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
  - tar -C /usr/local -xzf /tmp/go.tar.gz
  # Install VS Code Server
  - curl -fsSL https://code-server.dev/install.sh | sh
  - systemctl enable code-server@developer
  - systemctl start code-server@developer
EOF
      }

      resources {
        cpu    = 4000
        memory = 8192
      }

      # Mount for persistent development files
      volume_mount {
        volume      = "dev-workspace"
        destination = "/home/developer/workspace"
        read_only   = false
      }
    }

    network {
      port "ssh" {
        to = 22
      }
      port "web" {
        to = 8080  # VS Code Server
      }
      port "api" {
        to = 3000  # Development server
      }
    }

    service {
      name = "dev-environment"
      port = "ssh"
      tags = ["development", "ssh", "vscode"]

      check {
        type     = "tcp"
        interval = "30s"
        timeout  = "5s"
      }
    }
  }

  volume "dev-workspace" {
    type      = "host"
    source    = "dev-workspace"
    read_only = false
  }
}
```

## Batch Processing

### Data Processing Job

```hcl
job "data-processor" {
  datacenters = ["dc1"]
  type = "batch"

  group "processing" {
    task "process-data" {
      driver = "virt"

      template {
        data = <<EOF
#!/bin/bash
echo "Starting data processing job..."
echo "Processing files in /input directory"

# Simulate data processing
for file in /input/*.csv; do
  if [ -f "$file" ]; then
    echo "Processing $file"
    # Example: convert CSV to JSON
    python3 -c "
import csv, json, sys
with open('$file') as f:
    reader = csv.DictReader(f)
    data = list(reader)
filename = '${file##*/}'
output_file = '/output/' + filename.replace('.csv', '.json')
with open(output_file, 'w') as f:
    json.dump(data, f, indent=2)
print(f'Converted {filename} to JSON')
"
  fi
done

echo "Data processing complete!"
EOF
        destination = "local/process.sh"
        perms       = "755"
      }

      config {
        image = "/var/lib/images/python-slim.img"
        hostname = "data-processor"

        user_data = <<EOF
#cloud-config
packages:
  - python3
  - python3-pip

runcmd:
  - mkdir -p /input /output
  - pip3 install pandas numpy
  - cp /alloc/data/process.sh /usr/local/bin/
  - chmod +x /usr/local/bin/process.sh
  - /usr/local/bin/process.sh
EOF
      }

      resources {
        cpu    = 2000
        memory = 2048
      }

      # Input and output volumes
      volume_mount {
        volume      = "input-data"
        destination = "/input"
        read_only   = true
      }

      volume_mount {
        volume      = "output-data"
        destination = "/output"
        read_only   = false
      }
    }
  }

  volume "input-data" {
    type      = "host"
    source    = "batch-input"
    read_only = true
  }

  volume "output-data" {
    type      = "host"
    source    = "batch-output"
    read_only = false
  }
}
```

## Multi-tier Applications

### Complete Web Application Stack

```hcl
job "webapp-stack" {
  datacenters = ["dc1"]
  type = "service"

  # Database tier
  group "database" {
    task "postgres" {
      driver = "virt"

      config {
        image = "/var/lib/images/postgres-14.img"
        hostname = "webapp-db"

        use_thin_copy = true
        primary_disk_size = 10240

        network_interface {
          bridge {
            name = "br0"
            static_ip = "192.168.1.10"
            ports = ["db"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - postgresql

runcmd:
  - systemctl enable postgresql
  - systemctl start postgresql
  - sudo -u postgres createdb webapp
  - sudo -u postgres psql -c "CREATE USER webapp_user WITH PASSWORD 'webapp_pass';"
  - sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE webapp TO webapp_user;"
EOF
      }

      resources {
        cpu    = 1000
        memory = 1024
      }
    }

    network {
      port "db" {
        static = 5432
        to     = 5432
      }
    }

    service {
      name = "webapp-db"
      port = "db"
      tags = ["database", "postgres"]
    }
  }

  # Backend API tier
  group "backend" {
    count = 2

    task "api" {
      driver = "virt"

      template {
        data = <<EOF
const express = require('express');
const { Pool } = require('pg');

const app = express();
const port = 3000;

const pool = new Pool({
  user: 'webapp_user',
  host: '192.168.1.10',
  database: 'webapp',
  password: 'webapp_pass',
  port: 5432,
});

app.use(express.json());

app.get('/api/health', (req, res) => {
  res.json({ status: 'healthy', instance: process.env.HOSTNAME });
});

app.get('/api/users', async (req, res) => {
  try {
    const result = await pool.query('SELECT * FROM users');
    res.json(result.rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

app.listen(port, '0.0.0.0', () => {
  console.log(`API server running on port ${port}`);
});
EOF
        destination = "local/server.js"
      }

      config {
        image = "/var/lib/images/node-18.img"
        hostname = "webapp-api-${NOMAD_ALLOC_INDEX}"

        network_interface {
          bridge {
            name = "br0"
            ports = ["api"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - nodejs
  - npm

write_files:
  - path: /app/package.json
    content: |
      {
        "name": "webapp-api",
        "dependencies": {
          "express": "^4.18.0",
          "pg": "^8.8.0"
        }
      }

runcmd:
  - mkdir -p /app
  - cp /alloc/data/server.js /app/
  - cd /app && npm install
  - cd /app && node server.js
EOF
      }

      resources {
        cpu    = 1000
        memory = 512
      }
    }

    network {
      port "api" {
        to = 3000
      }
    }

    service {
      name = "webapp-api"
      port = "api"
      tags = ["api", "backend"]

      check {
        type     = "http"
        path     = "/api/health"
        interval = "10s"
        timeout  = "3s"
      }
    }
  }

  # Frontend tier
  group "frontend" {
    count = 1

    task "nginx" {
      driver = "virt"

      template {
        data = <<EOF
upstream backend {
  {{range service "webapp-api"}}
  server {{.Address}}:{{.Port}};
  {{end}}
}

server {
  listen 80;

  location / {
    root /var/www/html;
    index index.html;
  }

  location /api/ {
    proxy_pass http://backend;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
  }
}
EOF
        destination = "local/nginx.conf"
        change_mode = "restart"
      }

      config {
        image = "/var/lib/images/nginx.img"
        hostname = "webapp-frontend"

        network_interface {
          bridge {
            name = "br0"
            ports = ["http"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - nginx

write_files:
  - path: /var/www/html/index.html
    content: |
      <!DOCTYPE html>
      <html>
        <head><title>Web Application</title></head>
        <body>
          <h1>Web Application Frontend</h1>
          <div id="content">Loading...</div>
          <script>
            fetch('/api/health')
              .then(r => r.json())
              .then(data => {
                document.getElementById('content').innerHTML =
                  '<p>Backend Status: ' + data.status + '</p>' +
                  '<p>Instance: ' + data.instance + '</p>';
              });
          </script>
        </body>
      </html>

runcmd:
  - cp /alloc/data/nginx.conf /etc/nginx/conf.d/default.conf
  - systemctl enable nginx
  - systemctl start nginx
EOF
      }

      resources {
        cpu    = 500
        memory = 256
      }
    }

    network {
      port "http" {
        static = 80
        to     = 80
      }
    }

    service {
      name = "webapp-frontend"
      port = "http"
      tags = ["frontend", "nginx", "web"]

      check {
        type     = "http"
        path     = "/"
        interval = "10s"
        timeout  = "3s"
      }
    }
  }
}
```

## Advanced Configurations

### High-Performance Computing (HPC) Job

```hcl
job "hpc-simulation" {
  datacenters = ["dc1"]
  type = "batch"

  constraint {
    attribute = "${node.class}"
    value     = "compute"
  }

  group "simulation" {
    task "compute" {
      driver = "virt"

      config {
        image = "/var/lib/images/hpc-ubuntu.img"
        hostname = "hpc-worker"

        # Custom kernel optimized for compute
        kernel = "/boot/vmlinuz-hpc"
        initramfs = "/boot/initramfs-hpc.img"
        cmdline = "console=ttyS0 isolcpus=1-7 nohz_full=1-7 rcu_nocbs=1-7"

        # Large memory allocation
        primary_disk_size = 20480

        user_data = <<EOF
#cloud-config
packages:
  - gcc
  - gfortran
  - openmpi-bin
  - openmpi-dev
  - python3-numpy
  - python3-scipy
  - python3-matplotlib

write_files:
  - path: /app/simulation.py
    content: |
      import numpy as np
      import time
      from mpi4py import MPI

      comm = MPI.COMM_WORLD
      rank = comm.Get_rank()
      size = comm.Get_size()

      print(f"Process {rank} of {size} starting simulation")

      # Simulate compute-intensive work
      n = 10000
      matrix = np.random.random((n, n))
      start_time = time.time()

      # Matrix multiplication
      result = np.dot(matrix, matrix.T)

      end_time = time.time()
      print(f"Process {rank} completed in {end_time - start_time:.2f} seconds")

      # Save results
      np.save(f'/results/result_{rank}.npy', result)
    permissions: '0755'

runcmd:
  - mkdir -p /app /results
  - pip3 install mpi4py
  - cd /app && mpirun -np 4 python3 simulation.py
EOF
      }

      resources {
        cpu    = 8000  # 8 CPU cores
        memory = 16384 # 16GB RAM
      }

      # Mount for results output
      volume_mount {
        volume      = "hpc-results"
        destination = "/results"
        read_only   = false
      }
    }
  }

  volume "hpc-results" {
    type      = "host"
    source    = "hpc-output"
    read_only = false
  }
}
```

### Multi-GPU Machine Learning Cluster

```hcl
job "ml-cluster" {
  datacenters = ["dc1"]
  type = "service"

  constraint {
    attribute = "${node.class}"
    value     = "gpu-cluster"
  }

  # Parameter server
  group "ps" {
    task "parameter-server" {
      driver = "virt"

      config {
        image = "/var/lib/images/tensorflow-distributed.img"
        hostname = "ml-ps"

        network_interface {
          bridge {
            name = "br0"
            static_ip = "192.168.1.100"
            ports = ["ps"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - python3
  - python3-pip

runcmd:
  - pip3 install tensorflow numpy
  - python3 -c "
import tensorflow as tf
import json

cluster = {
    'worker': ['192.168.1.101:2222', '192.168.1.102:2222'],
    'ps': ['192.168.1.100:2222']
}

server = tf.distribute.Server(
    tf.train.ClusterSpec(cluster),
    job_name='ps',
    task_index=0
)

print('Parameter server started')
server.join()
"
EOF
      }

      resources {
        cpu    = 2000
        memory = 4096
      }
    }

    network {
      port "ps" {
        static = 2222
        to     = 2222
      }
    }

    service {
      name = "ml-parameter-server"
      port = "ps"
      tags = ["ml", "parameter-server", "distributed"]
    }
  }

  # Worker nodes with GPUs
  group "workers" {
    count = 2

    task "worker" {
      driver = "virt"

      config {
        image = "/var/lib/images/tensorflow-gpu.img"
        hostname = "ml-worker-${NOMAD_ALLOC_INDEX}"

        # GPU passthrough
        vfio_devices = ["10de:2204"]  # NVIDIA RTX 3080

        network_interface {
          bridge {
            name = "br0"
            static_ip = "192.168.1.${101 + NOMAD_ALLOC_INDEX}"
            ports = ["worker"]
          }
        }

        user_data = <<EOF
#cloud-config
packages:
  - python3
  - python3-pip
  - nvidia-driver-470

runcmd:
  - pip3 install tensorflow-gpu numpy
  - python3 -c "
import tensorflow as tf
import os

cluster = {
    'worker': ['192.168.1.101:2222', '192.168.1.102:2222'],
    'ps': ['192.168.1.100:2222']
}

task_index = int(os.environ.get('NOMAD_ALLOC_INDEX', 0))

server = tf.distribute.Server(
    tf.train.ClusterSpec(cluster),
    job_name='worker',
    task_index=task_index
)

print(f'Worker {task_index} started with GPU')
server.join()
"
EOF
      }

      resources {
        cpu    = 4000
        memory = 8192
        device "nvidia/gpu" {
          count = 1
        }
      }
    }

    network {
      port "worker" {
        to = 2222
      }
    }

    service {
      name = "ml-worker"
      port = "worker"
      tags = ["ml", "worker", "gpu", "distributed"]
    }
  }
}
```

These examples demonstrate the versatility and power of the Nomad Cloud Hypervisor driver for various use cases, from simple development environments to complex distributed systems with GPU acceleration. Each example includes proper resource allocation, networking, service discovery, and health checking configurations.