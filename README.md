# NetDevOps CLI

**NetDevOps CLI** is a unified command-line tool designed for full-stack NetDevOps automation. It combines Terraform-based GNS3 topology creation, dynamic Zero Touch Provisioning (ZTP), and Ansible-driven device configuration into one cohesive workflow.

This tool simplifies building, managing, and automating network labs and digital twins for learning, testing, or production automation.

In NetDevOps CLI, the YAML file acts as the single source of truth for defining the entire network topology. This includes network devices (with their MAC addresses, vendors, and images), management switches, cloud connections, and ZTP integration. Rather than manually specifying device counts, templates, and links through flags, the YAML file centralizes all configuration in a human-readable and version-controllable format. It drives topology creation, management network bridging, ZTP IP assignments, and Ansible inventory generation—ensuring consistency across every phase of the NetDevOps workflow.

> **Yes… it’s yet another wrapper.**  
> Because clearly, the world needed *one more CLI* to fuse Terraform, Ansible, YAML, and GNS3 into a single pipeline.  
>  
> I built **NetDevOps CLI** to explore one burning question:  
> **_“How far can we actually push network automation before it becomes self-aware?”_**
>  
> What started as a side project became a full-stack tool: spinning up virtual routers, generating dynamic configs, provisioning with ZTP, auto-wiring management networks, and wrapping it all in clean Terraform and Ansible logic.  
>  
> YAML isn’t just config here — **it’s gospel.** It drives everything: topology layout, startup scripts, vendor mapping, MAC-based DHCP, and post-boot automation.

---

## Features

> Under the hood, **NetDevOps CLI** wraps an actual Terraform provider — yes, one I built from scratch:  
> [terraform-provider-gns3](https://github.com/NetOpsChic/terraform-provider-gns3) 
>  
> So if you ever thought, *“Hey, wouldn’t it be cool if Terraform could speak fluent GNS3?”* — well, now it does.  
>  
> NetDevOps CLI just makes it less painful, less verbose, and way more fun to automate networks like a boss.


- **GNS3 Topology Deployment** via Terraform wrapper for infrastructure deployment (CLI or YAML)
- **Ansible Wrapper** for network device configuration
- **Dynamic Inventory** generation from ZTP server
- **Zero Touch Provisioning (ZTP)** integration with DHCP & TFTP
- **Auto-Bridge Mode** to build a management network automatically

---

## Requirements

- Go 1.21+
- Terraform v1.5+
- GNS3 Server (v2.2+)
- Docker (ZTP server)
- Ansible

---

## Installation

```bash
go build -o netdevops
```

---

## Commands & Usage

### Deploy a GNS3 Topology (CLI-based)

```bash
./netdevops gns3-deploy \
  --project "MyCustomProject" \
  --routers 3 \
  --switches 1 \
  --clouds 1 \
  --template c7200 \
  --links "R1:0/0-SW1:0/1,R2-SW1,R3-SW1,SW1:0/3-Cloud1:0/0"
```

Create a GNS3 project using CLI flags to define the number of routers, switches, clouds, template, and topology links.

---

### Deploy a GNS3 Topology from YAML

```bash
./netdevops gns3-deploy-yaml -c test.yaml
```

Use a YAML file to declare your full topology, including node types, templates, MAC addresses, interfaces, and links.

---

### Destroy the GNS3 Project

```bash
./netdevops gns-destroy
```

Tears down the Terraform-managed GNS3 project and deletes all associated resources.

---

### Generate Dynamic Ansible Inventory (from ZTP Server)

```bash
./netdevops gns3-inventory -c test.yaml
```

Queries the ZTP server for MAC-to-IP assignments and builds an Ansible inventory with proper `ansible_network_os` for each device.

---

### Configure Devices with Ansible

```bash
./netdevops gns3-configure \
  -c test.yaml \
  --inventory ansible-inventory.yaml
```

Executes device-specific Ansible playbooks using a YAML-based topology and a dynamic inventory. Supports multiple vendors and routing protocols.

> ✅ This command acts as an **Ansible wrapper**, enabling automation workflows for routers after ZTP provisioning.

---

### Auto-Bridge Topology with Management Network + ZTP

### [ZTP Server](https://github.com/NetOpsChic/ztp-server)

In NetDevOps CLI, the YAML file serves as the single source of truth for your entire network topology. It defines all network devices, MAC addresses, interface configurations, routing protocols, ZTP integration, and link structure in one place. This file drives topology creation, dynamic IP assignment via Kea DHCP, Ansible inventory generation, and startup configuration templating—ensuring a consistent and reproducible NetDevOps workflow.

The ZTP workflow is powered by a lightweight Docker-based ZTP server, which handles DHCP, TFTP, and startup config rendering based on MAC addresses. You can find the ZTP server implementation here:



```bash
./netdevops gns3-auto-bridge -c test.yaml
```

Automatically:
- Connects all routers to a management switch
- Links switch to cloud and ZTP nodes
- Generates a Terraform topology
- Imports existing nodes and links for safe state management

---

## Project Structure

```bash
.
├── netdevops                # CLI binary
├── main.go                  # Entry point
├── templates/               # Terraform templates
│   └── terraformTemplate.tmpl
├── terraform/               # Generated Terraform configs
│   └── auto.tfvars.json
├── startup-configs/         # Generated startup configs (per MAC)
│   └── R1.cfg, R2.cfg, ...
├── test.yaml                # Sample topology definition
└── README.md
```

---

## Contributing

Contributions are welcome. Please fork the repo, submit PRs with clear intent, and follow project formatting/style conventions.

---

## License

MIT License