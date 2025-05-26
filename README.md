# NetDevOps CLI for GNS3

**NetDevOps CLI** is a  command-line tool designed for full-stack NetDevOps automation in GNS3. It combines Terraform-based GNS3 topology creation, dynamic Zero Touch Provisioning (ZTP), and Ansible-driven device configuration into one cohesive workflow.

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
## ❓ Why CLI for GNS3?

### Why Not?

As we shift toward **Network Automation**, the first critical step is to define network topologies in a **declarative format** — ideally through a unified `topology.yaml` file.

Creating a **Terraform provider for GNS3** was a natural and powerful starting point. It brought infrastructure-as-code (IaC) control to a traditionally **UI-heavy lab environment**, enabling repeatable and version-controlled deployments.


## ⚠️ Terraform Provider for GNS3 & GitOps Limitations

Despite its strengths, Terraform — especially when used with GitOps — has some significant limitations in a GNS3 context.

### 🔁 "Push-Only" Nature

Terraform is **declarative**, but it’s not **self-healing** unless you explicitly run:

```bash
terraform apply
```

In a typical GitOps workflow:

1. You update a `.tf` file.
2. Commit it to Git.
3. CI/CD runs `terraform apply`.
4. The state file is updated.

However:

> 🚫 Nothing is monitoring the live GNS3 infrastructure for out-of-band changes (e.g., someone deleting a node via the GUI).

Unless you re-run Terraform, it doesn’t:

- Detect that a node disappeared.
- Automatically re-create the node.
- Warn you about drift.

This is known as the "**push-only**" model:

- Terraform pushes config into reality.
- But doesn’t **pull the current state** to compare unless you tell it to.

---

### 🧭 No Drift Detection (Unless Manually Triggered)

Imagine your YAML or `.tf` file still defines a switch called `mgmt_switch`, but someone manually deleted it in the GNS3 GUI.

GitOps + Terraform won’t notice unless:

1. You manually run:

```bash
terraform plan
```

2. It computes a diff between:
   - Desired state (code)
   - Current state (state file)

3. It proposes to recreate the missing switch.

> But even then, this assumes the state file is accurate and no other unexpected changes occurred.

There is:

- ❌ No built-in watcher.
- ❌ No automatic reconciliation loop.
- ❌ No feedback until you manually intervene.

---

### 🔄 No Continuous Reconciliation

Terraform **does not**:

- Watch `.tf` or YAML files for updates.
- Reconcile every few seconds.
- Detect or recover from **manual GUI edits** in GNS3.

---

## ✅ Solution: Reconcile Engine

To overcome this, a reconciliation engine was added:

- Watches the YAML topology file in real-time
- Periodically polls GNS3 for actual state
- Computes **delta changes** (added/removed nodes and links)
- Imports or deletes state in Terraform accordingly
- Maintains GNS3 topology in **desired state**

This brings:

- 🌐 GitOps-style automation
- 🔁 Continuous reconciliation
- 🛠️ Automated self-healing of drift

```bash
Start
 │
 ├──► Load YAML Topology File
 │       └── parse into Topology struct
 │
 ├──► Build Desired State
 │       ├── Nodes from routers, switches, clouds, templates
 │       └── Links from endpoint definitions
 │
 ├──► Reconcile Nodes
 │       ├── fetch current nodes from GNS3
 │       ├── compare with desired
 │       ├── create missing nodes
 │       └── delete extra nodes
 │
 ├──► Wait for Nodes to Become Available
 │
 ├──► Re-fetch Node List (with retries)
 │       └── map names to node IDs
 │
 ├──► Resolve Desired Links (ID-based)
 │       └── convert name-based to ID-based using nameToID map
 │
 ├──► Reconcile Links
 │       ├── fetch current links from GNS3
 │       ├── diff against desired
 │       ├── create missing links
 │       └── delete extra links
 │
 ├──► Terraform Delta Sync
 │       ├── Remove deleted resources from state
 │       ├── Import newly created nodes/links
 │       └── Clean up stale entries
 │
 └──► Done

```

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
- **Reconcilation loop and Terraform state file Update** 
- **Fixed scehma for input YAML**

---

## Requirements

- Go 1.21+
- Terraform v1.5+
- GNS3 Server (v2.2+) with Arista EOS
- Docker (ZTP server)
- Ansible

---

## Installation

```bash
go build -o netdevops
```

---

## Commands & Usage

### Notes & Validation Constraints

```bash
project:
  name:           # string, required
  start_nodes:    # boolean, required
  terraform_version: # string, required
  gns3_server:    # string, recommended

network-device:
  routers:
    - name:           # string, required, unique
      hostname:       # string, optional
      vendor:         # string, required (e.g., 'arista', 'cisco')
      mac_address:    # string, required (MAC format)
      image:          # string, required (disk image path)
      config:
        - interface:    # string, required if interface config
          ip_address:   # string, required if interface config
        - ospf:         # object, optional, only one per router
            router_id:  # string, required
            area:       # string, required
            networks:   # array of string, required
            interfaces: # array of objects
              - name:    # string
                cost:    # integer, optional
                passive: # boolean

switches:
  - name:         # string, required, unique

clouds:
  - name:         # string, required, unique

templates:
  servers:
    - name:           # string, required, unique
      ztp_server:     # string, optional, IP address
      observe-tower:  # string, optional, IP address
      start:          # boolean, required

links:
  - endpoints:
      - name:     # string, required
        adapter:  # integer, required
        port:     # integer, required
      - name:     # string, required
        adapter:  # integer, required
        port:     # integer, required
```

- All top-level keys (project, network-device, switches, clouds, templates, links) are required.

- Each router’s config is a list of interface or OSPF configuration blocks. Each interface config must have interface and ip_address.

- Only one ospf block should appear per router’s config.

- All arrays (routers, switches, clouds, links, etc.) should have unique name fields for identification.

- Each link.endpoints array must have exactly two entries.

- If ztp_server or observe-tower is set in a server template, they must be valid IPs.

### Deploy a GNS3 Topology from YAML

```bash
./netdevops gns3-deploy -c test.yaml -d
```

Use a YAML file to declare your full topology, including node types, templates, MAC addresses, interfaces, and links.
detach or -d flag will detach the process in background.

---

### Configure Devices with Ansible

```bash
./netdevops gns3-configure \
  -c test.yaml \
  --inventory ansible-inventory.yaml
```
Executes device-specific Ansible playbooks using a YAML-based topology and a dynamic inventory. Supports multiple vendors and routing protocols.

---

### Validation and monitoring

```bash
./netdevops gns3-validate \
  -c test.yaml \
  --inventory ansible-inventory.yaml
```
---
### Destroy the GNS3 Project

```bash
./netdevops gns-destroy -c topology --clean-up-all
```

Tears down the Terraform-managed GNS3 project and deletes all associated resources.

The --clean-up-all flags deletes the directory assosicated with the project.

---

## Contributing

Contributions are welcome. Please fork the repo, submit PRs with clear intent, and follow project formatting/style conventions.

---

## License

MIT License