// File: cmd/reconcile.go
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
)

// startReconcileDaemon watches the YAML, then runs an initial + on-change + periodic reconcile pass.
func StartReconcileDaemon(yamlPath, projectID string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ watcher error: %v\n", err)
		return
	}
	defer watcher.Close()

	dir := filepath.Dir(yamlPath)
	if err := watcher.Add(dir); err != nil {
		fmt.Fprintf(os.Stderr, "❌ watcher.Add(%s): %v\n", dir, err)
		return
	}

	// initial pass
	runReconcile(yamlPath, projectID)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case ev := <-watcher.Events:
			if filepath.Clean(ev.Name) == filepath.Clean(yamlPath) &&
				(ev.Op&fsnotify.Write|fsnotify.Create|fsnotify.Rename != 0) {
				fmt.Println("📄 topology.yaml changed; reconciling…")
				runReconcile(yamlPath, projectID)
			}
		case <-ticker.C:
			fmt.Println("⏱️  Periodic reconcile…")
			runReconcile(yamlPath, projectID)
		case <-stop:
			fmt.Println("\n🛑 Reconcile daemon stopped.")
			return
		}
	}
}

// runReconcile watches your YAML, reconciles GNS3, and then
// delta-syncs exactly the nodes you added or deleted.
func runReconcile(yamlPath, projectID string) {
	// 1) Load topology
	topo, err := loadTopology(yamlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ failed to load topology: %v\n", err)
		return
	}
	gns3Server = topo.Project.GNS3Server
	if gns3Server == "" {
		gns3Server = "http://localhost:3080" // or your actual default
	}
	// 2) Build desired nodes and links
	desiredNodes, desiredLinksByName := BuildDesired(topo)

	// 3) Reconcile nodes (create/delete)
	_, addedNodes, deletedNodes, err := reconcileNodes(desiredNodes, projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ node reconcile: %v\n", err)
		return
	}

	// 4) Fetch observed nodes and map names to IDs
	var obsNodes []ObservedNode
	for i := 0; i < 5; i++ {
		time.Sleep(time.Second)
		obsNodes, err = fetchNodesFromGNS3(projectID)
		if err == nil && len(obsNodes) > 0 {
			break
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ fetchNodes: %v\n", err)
		return
	}
	nameToID := buildNameToID(obsNodes)

	// 5) Build resolved desired link payloads
	var desiredLinks []LinkCreatePayload
	linkNameMap := make(map[string]string) // YAML name → Terraform resource name
	for _, ln := range desiredLinksByName {

		if len(ln.Nodes) != 2 {
			continue
		}
		var rp LinkCreatePayload
		ok := true
		var from, to string
		for i, ep := range ln.Nodes {
			id, found := nameToID[ep.NodeName]
			if !found {
				ok = false
				break
			}
			rp.Nodes = append(rp.Nodes, linkEndpoint{
				NodeID:        id,
				AdapterNumber: ep.AdapterNumber,
				PortNumber:    ep.PortNumber,
			})
			if i == 0 {
				from = ep.NodeName
			} else {
				to = ep.NodeName
			}
		}
		if ok {
			desiredLinks = append(desiredLinks, rp)
			linkNameMap[fmt.Sprintf("%s_to_%s", from, to)] = fmt.Sprintf("%s_to_%s", from, to)
		}
	}

	// 6) Reconcile links
	addedLinks, deletedLinks := reconcileLinksWithTracking(desiredLinks, projectID)

	// 7) Perform delta sync for both nodes and links
	if len(addedNodes) > 0 || len(deletedNodes) > 0 || len(addedLinks) > 0 || len(deletedLinks) > 0 {
		var toAdd, toDel []TerraformResource

		// Nodes: additions
		for _, nd := range addedNodes {
			if id, ok := nameToID[nd.Name]; ok {
				toAdd = append(toAdd, TerraformResource{
					Type: nd.ResourceType,
					Name: nd.Name,
					ID:   id,
				})
			}
		}

		// Nodes: deletions
		for _, od := range deletedNodes {
			toDel = append(toDel, TerraformResource{
				Type: gns3NodeTypeToTF(od.NodeType),
				Name: od.Name,
				ID:   od.ID,
			})
		}

		// Links: additions
		for _, l := range addedLinks {
			if len(l.Nodes) == 2 {
				fromID := l.Nodes[0].NodeID
				toID := l.Nodes[1].NodeID
				fromName := findNodeNameByID(obsNodes, fromID)
				toName := findNodeNameByID(obsNodes, toID)
				tfName := fmt.Sprintf("%s_to_%s", fromName, toName)
				toAdd = append(toAdd, TerraformResource{
					Type: "gns3_link",
					Name: tfName,
					ID:   l.ID,
				})
			}
		}

		// Links: deletions
		for _, l := range deletedLinks {
			if len(l.Nodes) == 2 {
				fromID := l.Nodes[0].NodeID
				toID := l.Nodes[1].NodeID
				fromName := findNodeNameByID(obsNodes, fromID)
				toName := findNodeNameByID(obsNodes, toID)
				tfName := fmt.Sprintf("%s_to_%s", fromName, toName)
				toDel = append(toDel, TerraformResource{
					Type: "gns3_link",
					Name: tfName,
					ID:   l.ID,
				})
			}
		}

		if err := syncTerraformDelta(topo.Project.Name, projectID, toAdd, toDel); err != nil {
			fmt.Fprintf(os.Stderr, "❌ terraform delta sync: %v\n", err)
		}
	}

	fmt.Println("✅ Reconcile pass complete")
}

// findNodeNameByID returns the node name for a given node ID.
func findNodeNameByID(nodes []ObservedNode, id string) string {
	for _, n := range nodes {
		if n.ID == id {
			return n.Name
		}
	}
	return id // fallback to ID if name not found
}

// reconcileLinksWithTracking reconciles links and returns added and deleted links with IDs.
func reconcileLinksWithTracking(desired []LinkCreatePayload, projectID string) (added []ObservedLink, deleted []ObservedLink) {
	observed, err := fetchLinksFromGNS3(projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ fetchLinksFromGNS3: %v\n", err)
		return
	}

	toAdd, toDel := diffLinks(desired, observed)

	for _, lp := range toAdd {
		fmt.Printf("➕ Creating link %+v…\n", lp.Nodes)
		if err := createLink(lp, projectID); err != nil {
			fmt.Fprintf(os.Stderr, "   ❌ createLink: %v\n", err)
		} else {
			// Re-fetch to obtain only the newly added link
			newLinks, err := fetchLinksFromGNS3(projectID)
			if err == nil {
				for _, nl := range newLinks {
					if len(nl.Nodes) == 2 {
						a, b := nl.Nodes[0].NodeID, nl.Nodes[1].NodeID
						ua, ub := lp.Nodes[0].NodeID, lp.Nodes[1].NodeID
						if (a == ua && b == ub) || (a == ub && b == ua) {
							added = append(added, nl)
							break
						}
					}
				}
			}
		}
	}

	for _, ol := range toDel {
		fmt.Printf("🗑️  Deleting link %s…\n", ol.ID)
		if err := deleteLink(ol.ID, projectID); err != nil {
			fmt.Fprintf(os.Stderr, "   ❌ deleteLink: %v\n", err)
		} else {
			deleted = append(deleted, ol)
		}
	}

	return
}

func syncTerraformDelta(
	projectName, projectID string,
	toAdd []TerraformResource,
	toDel []TerraformResource,
) error {
	tfDir := terraformDir(projectName)

	// Remove deleted resources
	for _, res := range toDel {
		addr := fmt.Sprintf("%s.%s", res.Type, res.Name)
		fmt.Printf("🗑️  Removing state for deleted %s\n", addr)
		rmCmd := exec.Command("terraform", "state", "rm", addr)
		rmCmd.Dir = tfDir
		if out, err := rmCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr,
				"⚠️ state rm %s failed: %v\n%s\n",
				addr, err, string(out),
			)
		} else {
			fmt.Printf("✅ state rm %s succeeded\n", addr)
		}
	}

	// Helper: remove any state entries matching prefix
	removeAnyMatching := func(prefix string) {
		listCmd := exec.Command("terraform", "state", "list")
		listCmd.Dir = tfDir
		out, err := listCmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️ state list failed: %v\n%s\n", err, out)
			return
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.HasPrefix(line, prefix) {
				fmt.Printf("🗑️  Removing stale state for %s\n", line)
				rm := exec.Command("terraform", "state", "rm", line)
				rm.Dir = tfDir
				if ro, err := rm.CombinedOutput(); err != nil {
					fmt.Fprintf(os.Stderr, "⚠️ stale rm %s failed: %v\n%s\n", line, err, string(ro))
				}
			}
		}
	}

	// Import added resources
	for _, res := range toAdd {
		fullAddr := fmt.Sprintf("%s.%s", res.Type, res.Name)

		// Determine import ID
		var importID string
		switch res.Type {
		case "gns3_qemu_node", "gns3_switch", "gns3_cloud", "gns3_template":
			importID = fmt.Sprintf("%s/%s", projectID, res.ID)
		case "gns3_link":
			if res.ID == "" {
				fmt.Printf("⚠️  Skipping link import %s: missing link ID\n", fullAddr)
				continue
			}
			importID = fmt.Sprintf("%s/%s", projectID, res.ID)
		default:
			fmt.Printf("⚠️  Skipping unknown resource type: %s\n", res.Type)
			continue
		}

		// Remove stale
		removeAnyMatching(fullAddr)
		removeAnyMatching("data.gns3_node_id." + res.Name)

		// Import
		fmt.Printf("📥 Importing %s → %s\n", fullAddr, importID)
		impCmd := exec.Command("terraform", "import", fullAddr, importID)
		impCmd.Dir = tfDir
		out, err := impCmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"⚠️ terraform import %s failed: %v\n%s\n",
				fullAddr, err, string(out),
			)
		} else {
			fmt.Printf("✅ Imported %s\n", fullAddr)
		}
	}

	return nil
}

func reconcileNodes(desired []NodeCreatePayload, projectID string) (changed bool, added []NodeCreatePayload, deleted []ObservedNode, err error) {
	// Fetch current GNS3 nodes
	observed, err := fetchNodesFromGNS3(projectID)
	if err != nil {
		return false, nil, nil, err
	}

	// Diff to find what to add/delete
	toAdd, toDel := diffRouters(desired, observed)

	// Create missing nodes
	for _, nd := range toAdd {
		fmt.Printf("➕ Creating node %s…\n", nd.Name)
		if err := createNode(nd, projectID); err != nil {
			fmt.Fprintf(os.Stderr, "   ❌ createNode: %v\n", err)
		} else {
			changed = true
			added = append(added, nd)
		}
	}

	// Delete extra nodes
	for _, o := range toDel {
		fmt.Printf("🗑️  Deleting node %s…\n", o.Name)
		if err := deleteNode(o.ID, projectID); err != nil {
			fmt.Fprintf(os.Stderr, "   ❌ deleteNode: %v\n", err)
		} else {
			changed = true
			deleted = append(deleted, o)
		}
	}

	// Ensure desired nodes are running
	desiredSet := make(map[string]struct{}, len(desired))
	for _, d := range desired {
		desiredSet[d.Name] = struct{}{}
	}
	for _, o := range observed {
		if _, want := desiredSet[o.Name]; want && o.Status != "started" {
			fmt.Printf("🔄 Starting node %s (was %s)…\n", o.Name, o.Status)
			if err := startNode(o.ID, projectID); err != nil {
				fmt.Fprintf(os.Stderr, "   ❌ startNode: %v\n", err)
			}
		}
	}

	return changed, added, deleted, nil
}

func reconcileLinks(desired []LinkCreatePayload, projectID string) (changed bool, added []LinkCreatePayload, deleted []ObservedLink, err error) {
	observed, err := fetchLinksFromGNS3(projectID)
	if err != nil {
		return false, nil, nil, err
	}

	toAdd, toDel := diffLinks(desired, observed)

	for _, lp := range toAdd {
		fmt.Printf("➕ Creating link %v…\n", lp.Nodes)
		if err := createLink(lp, projectID); err != nil {
			fmt.Fprintf(os.Stderr, "   ❌ createLink: %v\n", err)
		} else {
			changed = true
			added = append(added, lp)
		}
	}

	for _, ol := range toDel {
		fmt.Printf("🗑️  Deleting link %s…\n", ol.ID)
		if err := deleteLink(ol.ID, projectID); err != nil {
			fmt.Fprintf(os.Stderr, "   ❌ deleteLink: %v\n", err)
		} else {
			changed = true
			deleted = append(deleted, ol)
		}
	}

	return changed, added, deleted, nil
}

// listGlobalTemplates fetches all available global templates from GNS3.
func listGlobalTemplates() ([]Template, error) {
	url := fmt.Sprintf("%s/v2/templates", strings.TrimRight(gns3Server, "/"))
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, data)
	}
	var templates []Template
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

// loadTopology reads & unmarshals the YAML file into Topology.
func loadTopology(path string) (Topology, error) {
	var t Topology
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return t, err
	}
	return t, yaml.Unmarshal(data, &t)
}

// buildNameToID creates a map from both full names and their “prefix” (before last dash) → node ID.
func buildNameToID(nodes []ObservedNode) map[string]string {
	m := make(map[string]string, len(nodes)*2)
	for _, n := range nodes {
		m[n.Name] = n.ID
		if idx := strings.LastIndex(n.Name, "-"); idx != -1 {
			prefix := n.Name[:idx]
			if _, ok := m[prefix]; !ok {
				m[prefix] = n.ID
			}
		}
	}
	return m
}

func diffRouters(
	desired []NodeCreatePayload,
	observed []ObservedNode,
) (toAdd []NodeCreatePayload, toDel []ObservedNode) {
	used := make(map[string]bool)
	for _, d := range desired {
		found := false
		for _, o := range observed {
			if strings.HasPrefix(o.Name, d.Name) && !used[o.Name] {
				used[o.Name] = true
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, d)
		}
	}
	for _, o := range observed {
		matched := false
		for _, d := range desired {
			if strings.HasPrefix(o.Name, d.Name) {
				matched = true
				break
			}
		}
		if !matched {
			toDel = append(toDel, o)
		}
	}
	return
}

func diffLinks(
	desired []LinkCreatePayload,
	observed []ObservedLink,
) (toAdd []LinkCreatePayload, toDel []ObservedLink) {
	type key struct{ a, b string }
	obsSet := make(map[key]ObservedLink, len(observed))
	for _, o := range observed {
		if len(o.Nodes) == 2 {
			u, v := o.Nodes[0].NodeID, o.Nodes[1].NodeID
			if u > v {
				u, v = v, u
			}
			obsSet[key{u, v}] = o
		}
	}
	for _, d := range desired {
		if len(d.Nodes) != 2 {
			continue
		}
		u, v := d.Nodes[0].NodeID, d.Nodes[1].NodeID
		if u > v {
			u, v = v, u
		}
		k := key{u, v}
		if _, ok := obsSet[k]; !ok {
			toAdd = append(toAdd, d)
		}
	}
	desSet := make(map[key]struct{}, len(desired))
	for _, d := range desired {
		if len(d.Nodes) == 2 {
			u, v := d.Nodes[0].NodeID, d.Nodes[1].NodeID
			if u > v {
				u, v = v, u
			}
			desSet[key{u, v}] = struct{}{}
		}
	}
	for k, o := range obsSet {
		if _, keep := desSet[k]; !keep {
			toDel = append(toDel, o)
		}
	}
	return
}

// ————— GNS3 HTTP helpers —————

func fetchNodesFromGNS3(projectID string) ([]ObservedNode, error) {
	url := fmt.Sprintf("%s/v2/projects/%s/nodes", strings.TrimRight(gns3Server, "/"), projectID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, data)
	}
	var out []ObservedNode
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func fetchLinksFromGNS3(projectID string) ([]ObservedLink, error) {
	url := fmt.Sprintf("%s/v2/projects/%s/links", strings.TrimRight(gns3Server, "/"), projectID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, data)
	}
	var out []ObservedLink
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
func BuildDesired(t Topology) (nodes []NodeCreatePayload, links []LinkCreatePayload) {
	// Routers → QEMU nodes
	for _, r := range t.NetworkDevice.Routers {
		nodes = append(nodes, NodeCreatePayload{
			Name:         r.Name,
			TemplateName: "qemu",
			ResourceType: "gns3_qemu_node",
			Properties: map[string]interface{}{
				"name":           r.Name,
				"adapter_type":   "e1000",
				"hda_disk_image": r.Image,
				"node_type":      "qemu",
				"compute_id":     "local",
				"mac_address":    r.MacAddress,
				"adapters":       10,
				"ram":            2048,
				"cpus":           2,
				"start_vm":       true,
				"platform":       "x86_64",
				"console_type":   "telnet",
			},
		})
	}

	// Switches
	for _, s := range t.Switches {
		nodes = append(nodes, NodeCreatePayload{
			Name:         s.Name,
			TemplateName: "ethernet_switch",
			TemplateID:   "ethernet_switch",
			ResourceType: "gns3_switch",
		})
	}

	// Clouds
	for _, c := range t.Clouds {
		nodes = append(nodes, NodeCreatePayload{
			Name:         c.Name,
			TemplateName: "cloud",
			TemplateID:   "cloud",
			ResourceType: "gns3_cloud",
		})
	}

	// Template-based servers
	for _, srv := range t.Templates.Servers {
		nodes = append(nodes, NodeCreatePayload{
			Name:         srv.Name,
			TemplateName: srv.Name,
			ResourceType: "gns3_template",
		})
	}

	// Links
	for _, l := range t.Links {
		if len(l.Endpoints) != 2 {
			fmt.Fprintf(os.Stderr, "❌ Skipping link with %d endpoints: %+v\n", len(l.Endpoints), l)
			continue
		}
		if l.Endpoints[0].Name == "" || l.Endpoints[1].Name == "" {
			fmt.Fprintf(os.Stderr, "❌ Skipping link with missing endpoint name: %+v\n", l)
			continue
		}

		var lp LinkCreatePayload
		for _, ep := range l.Endpoints {
			lp.Nodes = append(lp.Nodes, linkEndpoint{
				NodeName:      ep.Name,
				AdapterNumber: ep.Adapter,
				PortNumber:    ep.Port,
			})
		}
		links = append(links, lp)
	}

	return
}

func deleteNode(nodeID, projectID string) error {
	req, _ := http.NewRequest("DELETE",
		fmt.Sprintf("%s/v2/projects/%s/nodes/%s", strings.TrimRight(gns3Server, "/"), projectID, nodeID),
		nil,
	)
	_, err := http.DefaultClient.Do(req)
	return err
}

func startNode(nodeID, projectID string) error {
	url := fmt.Sprintf("%s/v2/projects/%s/nodes/%s/start", strings.TrimRight(gns3Server, "/"), projectID, nodeID)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("start API %d: %s", resp.StatusCode, data)
	}
	return nil
}

func createNode(nd NodeCreatePayload, projectID string) error {
	var url string
	var body []byte

	switch nd.TemplateName {
	case "cloud":
		url = fmt.Sprintf("%s/v2/projects/%s/nodes", strings.TrimRight(gns3Server, "/"), projectID)
		body, _ = json.Marshal(map[string]interface{}{
			"name":       nd.Name,
			"node_type":  "cloud",
			"compute_id": "local",
		})

	case "ethernet_switch":
		url = fmt.Sprintf("%s/v2/projects/%s/nodes", strings.TrimRight(gns3Server, "/"), projectID)
		body, _ = json.Marshal(map[string]interface{}{
			"name":       nd.Name,
			"node_type":  "ethernet_switch",
			"compute_id": "local",
		})

	case "qemu":
		// Controller-level QEMU node creation
		url = fmt.Sprintf("%s/v2/projects/%s/nodes", strings.TrimRight(gns3Server, "/"), projectID)

		// Step 1: Build payload
		payload := map[string]interface{}{
			"name":       nd.Name,
			"node_type":  "qemu",
			"compute_id": "local",
			"properties": map[string]interface{}{
				"adapter_type":   "e1000",
				"adapters":       10,
				"hda_disk_image": nd.Properties["image"],
				"mac_address":    nd.Properties["mac_address"],
				"ram":            2048,
				"cpus":           2,
				"platform":       "x86_64",
				"console_type":   "telnet",
			},
		}

		body, _ = json.Marshal(payload)

		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("QEMU node POST failed: %v", err)
		}
		defer resp.Body.Close()

		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			return fmt.Errorf("QEMU create API %d: %s", resp.StatusCode, data)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return fmt.Errorf("decode QEMU create response: %v", err)
		}

		nodeID, ok := result["node_id"].(string)
		if !ok || nodeID == "" {
			nodeID, ok = result["id"].(string)
			if !ok || nodeID == "" {
				return fmt.Errorf("QEMU create: missing node_id in response: %s", data)
			}
		}

		// Step 2: Start the QEMU node
		startURL := fmt.Sprintf("%s/v2/projects/%s/nodes/%s/start", strings.TrimRight(gns3Server, "/"), projectID, nodeID)
		startResp, err := http.Post(startURL, "application/json", nil)
		if err != nil {
			return fmt.Errorf("start QEMU node failed: %v", err)
		}
		defer startResp.Body.Close()

		startData, _ := io.ReadAll(startResp.Body)
		if startResp.StatusCode >= 300 {
			return fmt.Errorf("start QEMU node API %d: %s", startResp.StatusCode, startData)
		}

		fmt.Printf("🚀 QEMU node %q created and started successfully.\n", nd.Name)
		return nil

	default:
		// Template-based node
		templates, err := listGlobalTemplates()
		if err != nil {
			return fmt.Errorf("listGlobalTemplates error: %v", err)
		}

		var templateID string
		for _, t := range templates {
			if t.Name == nd.TemplateName {
				templateID = t.TemplateID
				break
			}
		}
		if templateID == "" {
			return fmt.Errorf("template %q doesn't exist", nd.TemplateName)
		}

		url = fmt.Sprintf("%s/v2/projects/%s/templates/%s", strings.TrimRight(gns3Server, "/"), projectID, templateID)
		body, _ = json.Marshal(map[string]interface{}{
			"name": nd.Name,
			"x":    0,
			"y":    0,
		})

		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("template node POST failed: %v", err)
		}
		defer resp.Body.Close()

		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			return fmt.Errorf("template-create API %d: %s", resp.StatusCode, data)
		}

		var node map[string]interface{}
		if err := json.Unmarshal(data, &node); err != nil {
			return fmt.Errorf("decode node JSON: %v", err)
		}
		nodeID, _ := node["node_id"].(string)
		if nodeID == "" {
			nodeID, _ = node["id"].(string)
		}
		if nodeID == "" {
			return fmt.Errorf("template node: missing node_id in response")
		}

		// Start the template-based node
		startURL := fmt.Sprintf("%s/v2/projects/%s/nodes/%s/start", strings.TrimRight(gns3Server, "/"), projectID, nodeID)
		startResp, err := http.Post(startURL, "application/json", nil)
		if err != nil {
			return fmt.Errorf("template node start POST failed: %v", err)
		}
		defer startResp.Body.Close()
		startData, _ := io.ReadAll(startResp.Body)
		if startResp.StatusCode >= 300 {
			return fmt.Errorf("template node start API %d: %s", startResp.StatusCode, startData)
		}

		fmt.Printf("🚀 Template node %q started successfully.\n", nd.Name)
		return nil
	}

	// Fallback (cloud/switch)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("raw node POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("raw node API %d: %s", resp.StatusCode, data)
	}

	fmt.Printf("✅ Raw node %q created.\n", nd.Name)
	return nil
}

func createLink(lp LinkCreatePayload, projectID string) error {
	url := fmt.Sprintf("%s/v2/projects/%s/links", strings.TrimRight(gns3Server, "/"), projectID)
	body, _ := json.Marshal(lp)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("link-create API %d: %s", resp.StatusCode, data)
	}
	return nil
}

func deleteLink(linkID, projectID string) error {
	req, _ := http.NewRequest("DELETE",
		fmt.Sprintf("%s/v2/projects/%s/links/%s", strings.TrimRight(gns3Server, "/"), projectID, linkID),
		nil,
	)
	_, err := http.DefaultClient.Do(req)
	return err
}

func terraformDir(projectName string) string {
	return filepath.Join("projects", projectName, "terraform")
}
func gns3NodeTypeToTF(nodeType string) string {
	switch nodeType {
	case "qemu":
		return "gns3_qemu_node"
	case "ethernet_switch":
		return "gns3_switch"
	case "cloud":
		return "gns3_cloud"
	default:
		return "gns3_template"
	}
}
