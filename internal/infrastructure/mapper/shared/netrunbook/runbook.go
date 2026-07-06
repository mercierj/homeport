package netrunbook

import domainrunbook "github.com/homeport/homeport/internal/domain/runbook"

func Routing(name, source string) []domainrunbook.Step {
	metadata := meta("routing", name, source)
	return []domainrunbook.Step{
		command("render-traefik-routes", "Render Traefik routes", "Routing", []string{"sh", "-c", "echo validate generated traefik dynamic config"}, "host, path, method, header, redirect, timeout, and auth hooks are represented or blocked", metadata),
		command("validate-route-table", "Validate route table", "Validate", []string{"sh", "-c", "echo replay generated HTTP route probes"}, "generated HTTP requests reach expected backends", metadata),
		input("block-unsupported-route-features", "Block unsupported route features", "Validate", "unsupported auth transforms or rewrites are marked guided before cutover", metadata),
		rollback("rollback-routing-source-authority", "Keep source routing authoritative", metadata),
	}
}

func DNS(zone, source, exportScript string) []domainrunbook.Step {
	metadata := meta("dns", zone, source)
	return []domainrunbook.Step{
		command("export-dns-zone", "Export DNS zone", "Discovery", script(exportScript, "echo export DNS records"), "source DNS records are exported", metadata),
		command("provision-local-dns-zone", "Provision local DNS zone", "Deploy", []string{"sh", "-c", "echo provision CoreDNS or PowerDNS zone"}, "CoreDNS or PowerDNS serves generated zone records", metadata),
		command("publish-external-dns-records", "Publish external DNS records", "Cutover", []string{"sh", "cutover_dns.sh"}, "exact NS, A, CNAME, and TXT records are rendered for registrar or authoritative DNS", metadata),
		command("poll-public-dns", "Poll public DNS", "Validate", []string{"sh", "validate_dns.sh"}, "internal and external DNS resolution match expected records", metadata),
		rollback("rollback-dns-source-authority", "Keep source DNS authoritative", metadata),
	}
}

func Edge(name, source string) []domainrunbook.Step {
	metadata := meta("edge", name, source)
	return []domainrunbook.Step{
		command("render-edge-cache-config", "Render edge cache config", "Edge", []string{"sh", "-c", "echo validate Caddy Varnish Traefik cache config"}, "origins, custom domains, TLS, compression, and cache TTLs are represented", metadata),
		input("handle-edge-functions", "Handle edge functions", "Edge", "Lambda@Edge or edge functions converted or marked guided", metadata),
		command("validate-cache-behavior", "Validate cache behavior", "Validate", []string{"sh", "-c", "echo validate cache hit miss and origin fallback"}, "cache hit, miss, TTL, and origin fallback probes pass", metadata),
		rollback("rollback-edge-source-authority", "Keep source CDN authoritative", metadata),
	}
}

func Network(name, source string) []domainrunbook.Step {
	metadata := meta("network", name, source)
	return []domainrunbook.Step{
		command("render-network-config", "Render network config", "Network", []string{"sh", "-c", "echo render docker networks subnets routes"}, "VPC or VNet subnet and routing intent is generated", metadata),
		command("render-firewall-rules", "Render firewall rules", "Network", []string{"sh", "-c", "echo render host firewall security group waf rules"}, "host firewall, security group, WAF, or Cloud Armor rules are generated or marked guided", metadata),
		command("validate-network-flows", "Validate network flows", "Validate", []string{"sh", "-c", "echo validate allowed and denied flows"}, "allowed flows pass and denied flows fail", metadata),
		rollback("rollback-network-source-authority", "Keep source network authoritative", metadata),
	}
}

func Observability(name, source string) []domainrunbook.Step {
	metadata := meta("observability", name, source)
	return []domainrunbook.Step{
		command("render-log-agent-config", "Render log agent config", "Logs", []string{"sh", "-c", "echo validate promtail loki cloudwatch logs adapter config"}, "generated agents, env, and compatibility endpoints replace manual logging notes", metadata),
		command("render-prometheus-scrapes", "Render Prometheus scrapes", "Metrics", []string{"sh", "-c", "echo generate prometheus scrape config from app units"}, "Prometheus scrape config covers migrated app units and exporters", metadata),
		command("validate-observability-ingestion", "Validate observability ingestion", "Validate", []string{"sh", "-c", "echo validate Loki log stream and Prometheus targets"}, "logs ingest and metric targets are up", metadata),
		rollback("rollback-observability-source-authority", "Keep source observability authoritative", metadata),
	}
}

func Alerts(name, source, testScript string) []domainrunbook.Step {
	metadata := meta("alerts", name, source)
	return []domainrunbook.Step{
		command("render-alert-rules", "Render alert rules", "Alerts", []string{"sh", "-c", "echo validate prometheus alert and alertmanager config"}, "supported CloudWatch metrics are converted and unsupported metrics list exact missing signals", metadata),
		command("validate-alert-route", "Validate alert route", "Validate", script(testScript, "echo fire test alert route"), "alert rule syntax validates and test alert reaches receiver", metadata),
		rollback("rollback-alert-source-authority", "Keep source alerting authoritative", metadata),
	}
}

func meta(kind, name, source string) map[string]string {
	return map[string]string{"kind": kind, "name": name, "source": source}
}

func script(path, fallback string) []string {
	if path == "" {
		return []string{"sh", "-c", fallback}
	}
	return []string{"sh", path}
}

func command(id, name, group string, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeCommand,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		SuccessCondition: success,
		Command:          command,
		Metadata:         clone(metadata),
	}
}

func input(id, name, group, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeInput,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "user",
		SuccessCondition: success,
		Metadata:         clone(metadata),
	}
}

func rollback(id, name string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            "Rollback",
		Type:             domainrunbook.StepTypeRollback,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "noop",
		SuccessCondition: "source remains authoritative until cutover passes",
		Metadata:         clone(metadata),
	}
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
