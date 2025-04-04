// File: cmd/ConfigureAristaTemplate.go
package cmd

// ConfigureAristaTemplate is an Ansible playbook template that configures
// IPv4 interfaces, IPv4 static routes, and OSPFv3 routing (including redistribution)
// using Arista EOS modules.
const ConfigureAristaTemplate = `- hosts: {{ .RouterName }}
  gather_facts: no
  connection: network_cli

  tasks:
    - name: Set device hostname
      arista.eos.eos_config:
        lines:
          - hostname {{ .RouterName }}

    - name: Configure IPv4 on interfaces
      arista.eos.eos_l3_interfaces:
        config:
{{- range .IPConfigs }}
          - name: {{ .Interface }}
            ipv4:
              - address: {{ .IPAddress }}
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
    - name: Configure OSPFv3 routing process
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
                  {{- if .OSPFv3.Redistribute }}
                  redistribute:
{{- range .OSPFv3.Redistribute }}
                    - routes: "{{ .Protocol }}"
{{- if .RouteMap }}
                      route_map: "{{ .RouteMap }}"
{{- end }}
{{- end }}
                  {{- end }}
                - afi: "ipv6"
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
{{- end }}
    - name: Save configuration
      arista.eos.eos_config:
        save_when: changed
`
