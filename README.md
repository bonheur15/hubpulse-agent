# HubPulse Agent

**Lightweight. Secure. Resilient.**

The HubPulse Agent is a high-performance Go-based monitoring binary designed to provide deep visibility into your server infrastructure. It collects system metrics, monitors internal services, and tails logs—all while maintaining a zero-inbound-port security posture.

---

## Quick Install

Install and start the agent with a single command:

```bash
curl -sSL https://install.HubPulse.space/script.sh | sudo bash -s -- --token=YOUR_AGENT_TOKEN
```

This script automatically detects your architecture (amd64/arm64), installs the binary to `/usr/local/bin`, configures the systemd service, and starts the agent.

---

## Core Features

### Security First
- **Zero Open Ports**: The agent only makes outbound HTTPS connections. It is invisible to external scanners.
- **Token Authentication**: Secure, unique tokens for every server identity.
- **Minimal Footprint**: Low CPU and RAM overhead, written in memory-safe Go.

### Comprehensive Monitoring
- **System Metrics**: CPU (load/usage), Memory, Disk I/O, Network throughput, and Uptime.
- **Process Tracking**: Monitor top resource-consuming processes with configurable filters.
- **Internal Service Probes**: Perform HTTP(S) and TCP health checks on internal services (e.g., databases, local APIs) that aren't exposed to the internet.
- **Log Collection**: Tail and forward system or application logs with configurable filtering and size limits.

### Resilience
- **Offline Buffering**: If the HubPulse collector is unreachable, the agent stores metrics locally in an encrypted spool and syncs them automatically when connectivity returns.
- **Self-Healing**: Runs as a systemd service with automatic restarts on failure.
- **Safe Config**: Validates and sanitizes configurations; falls back to safe defaults if a config file is corrupt.

---

## Command Line Interface

The agent provides a powerful CLI for local management:

| Command | Description |
|---------|-------------|
| `run` | Starts the agent collection loops (default: uses `/etc/hubpulse-agent/config.json`). |
| `status` | Performs a single collection and prints the result to stdout for debugging. |
| `validate-config` | Checks your local JSON configuration for errors without starting the agent. |
| `update-config` | Updates the local config from a Base64 payload (used by the Web UI). |
| `self-update` | Downloads and installs the latest version of the HubPulse Agent binary. |
| `version` | Prints the current agent version. |

---

## Configuration Example (`config.json`)

The agent is typically managed remotely from the HubPulse Dashboard, but you can manually edit `/etc/hubpulse-agent/config.json`:

```json
{
  "agent_id": "production-web-01",
  "token": "hp_...",
  "collector_url": "https://collector.hubpulse.space/ingest",
  "collection": {
    "metrics_interval": "15s",
    "service_interval": "30s"
  },
  "services": [
    {
      "name": "Local API",
      "type": "http",
      "target": "http://localhost:8080/health",
      "expected_statuses": [200, 204]
    }
  ],
  "logs": [
    {
      "name": "Nginx Access",
      "path": "/var/log/nginx/access.log",
      "enabled": true
    }
  ]
}
```

---

## Lifecycle Management

**Restart the agent:**
```bash
sudo systemctl restart hubpulse-agent
```

**Check logs:**
```bash
journalctl -u hubpulse-agent -f
```

**Update the agent:**
```bash
sudo hubpulse-agent self-update
sudo systemctl restart hubpulse-agent
```

**Uninstall the agent:**
```bash
curl -sSL https://install.HubPulse.space/uninstall.sh | sudo bash
```

---
Built by the Hubfly.space Team.
