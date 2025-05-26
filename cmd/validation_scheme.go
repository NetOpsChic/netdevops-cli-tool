// cmd/validate.go
package cmd

import (
	"fmt"
	"strings"
)

// validateTopology walks your Topology struct and accumulates any errors.
func validateTopology(t *Topology) error {
	var errs []string

	// Project
	if t.Project.Name == "" {
		errs = append(errs, "project.name is required")
	}
	if t.Project.TerraformVersion == "" {
		errs = append(errs, "project.terraform_version is required")
	}

	// Routers
	if len(t.NetworkDevice.Routers) == 0 {
		errs = append(errs, "network-device.routers must contain at least one router")
	} else {
		for i, r := range t.NetworkDevice.Routers {
			p := fmt.Sprintf("network-device.routers[%d]", i)
			if r.Name == "" {
				errs = append(errs, p+".name is required")
			}
			if r.Hostname == "" {
				errs = append(errs, p+".hostname is required")
			}
			switch strings.ToLower(r.Vendor) {
			case "arista", "cisco", "juniper":
			default:
				errs = append(errs, p+".vendor must be one of arista,cisco,juniper")
			}
			if r.MacAddress == "" {
				errs = append(errs, p+".mac_address is required")
			}
			if r.Image == "" {
				errs = append(errs, p+".image is required")
			}

			// r.Config is interface{} → assert to []interface{} before len()
			rawCfg := r.Config
			cfgSlice, ok := rawCfg.([]interface{})
			if !ok {
				errs = append(errs, p+".config is not an array")
			} else if len(cfgSlice) == 0 {
				errs = append(errs, p+".config must have at least one entry")
			}
		}
	}

	// Templates → Servers
	if len(t.Templates.Servers) == 0 {
		errs = append(errs, "templates.servers must contain at least one entry")
	} else {
		for i, srv := range t.Templates.Servers {
			p := fmt.Sprintf("templates.servers[%d]", i)
			if srv.Name == "" {
				errs = append(errs, p+".name is required")
			}
		}
	}

	// Links
	if len(t.Links) == 0 {
		errs = append(errs, "links must contain at least one link")
	} else {
		for i, link := range t.Links {
			p := fmt.Sprintf("links[%d].endpoints", i)
			if len(link.Endpoints) < 2 {
				errs = append(errs, p+" must have at least two endpoints")
			}
		}
	}

	// Switches & clouds
	for i, sw := range t.Switches {
		if sw.Name == "" {
			errs = append(errs, fmt.Sprintf("switches[%d].name is required", i))
		}
	}
	for i, cl := range t.Clouds {
		if cl.Name == "" {
			errs = append(errs, fmt.Sprintf("clouds[%d].name is required", i))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation failed:\n - %s", strings.Join(errs, "\n - "))
	}
	return nil
}
