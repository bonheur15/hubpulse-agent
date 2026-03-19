# HubPulse Agent

Go-based monitoring agent for HubPulse. The agent collects host metrics, process data, and internal service health, then ships resilient batches to the HubPulse collector without exposing inbound ports.

## Commands

- `hubpulse-agent run --config /etc/hubpulse-agent/config.json`
- `hubpulse-agent status --config /etc/hubpulse-agent/config.json`
- `hubpulse-agent validate-config --config /etc/hubpulse-agent/config.json`
- `hubpulse-agent init-config --config /etc/hubpulse-agent/config.json`
- `hubpulse-agent update-config --config /etc/hubpulse-agent/config.json "<base64>"`

## Implemented

- Safe JSON config loading with sanitization, defaults, and automatic reloads
- Host metrics for CPU, memory, swap, load, uptime, disk usage, disk I/O, and network activity
- Process inspection with top resource consumers and include/exclude filters
- Internal HTTP and TCP health checks with response validation and transition tracking
- Batched HTTPS delivery with retries, gzip, and offline spool buffering when the collector is unavailable
- CLI utilities for one-shot status, config validation, default config generation, and base64 config updates
