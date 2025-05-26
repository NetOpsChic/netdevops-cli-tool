package cmd

const ValidatePingTemplate = `- hosts: {{ .Source }}
  gather_facts: no
  connection: network_cli

  tasks:
    - name: Ping {{ .TargetIP }} from {{ .Source }}
      arista.eos.eos_command:
        commands:
          - ping {{ .TargetIP }}
      register: ping_result

    - name: Show ping output
      debug:
        var: ping_result.stdout_lines

    - name: Assert ping to {{ .TargetIP }} is successful
      assert:
        that:
          - ping_result.stdout[0] is search("0% packet loss")
`
const EnableEapiTemplate = `
- name: Enable eAPI for NetDevOps Observer
  hosts: all
  gather_facts: no
  connection: network_cli

  tasks:
    - name: Enable Arista eAPI via CLI
      arista.eos.eos_config:
        lines:
          - protocol http
          - no protocol https
          - no shutdown
        parents: management api http-commands
        match: strict
`
