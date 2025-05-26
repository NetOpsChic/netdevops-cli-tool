// File: cmd/ConfigureAristaTemplate.go
package cmd

// ConfigureAristaTemplate is an Ansible playbook template for Arista devices.
// It enables IP routing, brings up physical interfaces as L3, applies IPv4,
// static routes, OSPFv2 and BGP from your YAML.
const ConfigureAristaTemplate = `- hosts: {{ .RouterName }}
  gather_facts: no
  connection: network_cli

  tasks:
    - name: Enable global IP routing
      arista.eos.eos_config:
        lines:
          - ip routing

    - name: Configure L3 interfaces
      arista.eos.eos_interfaces:
        config:
{{- range .IPConfigs }}
{{- if ne (lower .Interface) "loopback1" }}
          - name: "{{ .Interface }}"
            enabled: true
            mode: layer3
{{- end }}
{{- end }}
        state: merged

    - name: Configure IPv4 on interfaces
      arista.eos.eos_l3_interfaces:
        config:
{{- range .IPConfigs }}
          - name: "{{ .Interface }}"
            ipv4:
              - address: "{{ .IPAddress }}"
{{- end }}
        state: merged

{{- if .StaticRoutes }}
    - name: Merge IPv4 static route configuration
      arista.eos.eos_static_routes:
        config:
{{- range .StaticRoutes }}
          - vrf: default
            address_families:
              - afi: ipv4
                routes:
                  - dest: "{{ .DestNetwork }}/{{ maskToPrefix .SubnetMask }}"
                    next_hops:
                      - forward_router_address: "{{ .NextHop }}"
{{- if .Interface }}
                        interface: "{{ .Interface }}"
{{- end }}
{{- end }}
        state: merged
{{- end }}

{{- if .OSPF }}
    - name: Configure OSPFv2 process
      arista.eos.eos_ospfv2:
        config:
          processes:
            - process_id: 1
              router_id: "{{ .OSPF.RouterID }}"
              networks:
{{- range .OSPF.Networks }}
                - prefix: "{{ . }}"
                  area:   "{{ $.OSPF.Area }}"
{{- end }}
        state: merged

    - name: Bind interfaces to OSPFv2
      arista.eos.eos_ospf_interfaces:
        config:
{{- range .OSPF.Interfaces }}
{{- if ne (lower .Name) "loopback1" }}
          - name: "{{ .Name }}"
            address_family:
              - afi: ipv4
                area:
                  area_id: "{{ $.OSPF.Area }}"
{{- if gt .Cost 0 }}
                cost: {{ .Cost }}
{{- end }}
{{- end }}
{{- end }}
        state: merged
{{- end }}

{{- if .BGP }}
    - name: Configure BGP global settings
      arista.eos.eos_bgp_global:
        config:
          as_number: "{{ .BGP.LocalAS }}"
          router_id: "{{ .BGP.RouterID }}"
          neighbor:
            - peer: "{{ .BGP.Neighbor }}"
              remote_as: "{{ .BGP.RemoteAS }}"
          network:
{{- range .BGP.Networks }}
            - address: "{{ . }}"
{{- end }}
          redistribute:
{{- range .BGP.Redistribute }}
            - protocol: "{{ .Protocol | lower }}"
{{- if .RouteMap }}
              route_map: "{{ .RouteMap }}"
{{- end }}
{{- if .IsisLevel }}
              isis_level: "{{ .IsisLevel }}"
{{- end }}
{{- if .OspfRoute }}
              ospf_route: "{{ .OspfRoute }}"
{{- end }}
{{- end }}
    - name: Save configuration
      arista.eos.eos_config:
        save_when: changed
{{- end }}
`
