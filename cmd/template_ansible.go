package cmd

// ConfigureStaticRoutingTemplate is an Ansible playbook template that configures
// IPv4 addresses on interfaces and IPv4 static routes using Arista EOS modules.
// For static routes, the YAML input only needs three (or optionally four) arguments:
// dest_network, subnet_mask, and next_hop (with an optional interface).
// The template uses a custom function maskToPrefix to convert the subnet mask into a CIDR prefix.
const ConfigureAristaTemplate = `- hosts: all
  gather_facts: no
  connection: network_cli

  tasks:
    - name: Configuring IPv4 on interfaces.
      arista.eos.eos_l3_interfaces:
        config:
{{- range .IPConfigs }}
          - name: {{ .Interface }}
            ipv4:
              - address: {{ .IPAddress }}
{{- end }}
        state: merged

{{- if .StaticRoutes }}
    - name: Merging IPv4 static route configuration.
      arista.eos.eos_static_routes:
        config:
{{- range .StaticRoutes }}
          - vrf: default
            address_families:
              - afi: ipv4
                routes:
                  - dest: {{ .DestNetwork }}/{{ maskToPrefix .SubnetMask }}
                    next_hops:
                      - forward_router_address: {{ .NextHop }}
{{- if .Interface }}
                        interface: {{ .Interface }}
{{- end }}
{{- end }}
        state: merged
{{- end }}
{{- if .OSPFv3 }}
    - name: Configuring OSPFv3 routing process.
      arista.eos.eos_ospfv3:
        config:
          processes:
            - router_id: "{{ .OSPFv3.RouterID }}"
              vrf: default
              address_family:
                - afi: "ipv4"
                  areas:
                    - area_id: "{{ .OSPFv3.Area }}"
                      ranges:
{{- range .OSPFv3.Networks }}
                          - address: "{{ cidrSubnetAddress . }}"
                            subnet_mask: "{{ cidrToMask . }}"
{{- end }}
                      {{- if eq (printf "%t" .OSPFv3.Stub) "true" }}
                      stub: {}
                      {{- end }}
                      {{- if eq (printf "%t" .OSPFv3.NSSA) "true" }}
                      nssa:
                        default_information_originate: true
                        no_summary: false
                      {{- end }}
        state: merged
{{- end }}



`
