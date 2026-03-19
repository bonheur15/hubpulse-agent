# HubPulse Agent

Go-based monitoring agent for HubPulse. The agent collects host metrics, process data, and internal service health, then ships resilient batches to the HubPulse collector without exposing inbound ports.

## Current Scope

- Bootstrap the Go module and command layout
- Add progress tracking in `progress.md` and `todo.md`
- Build a resilient collection, buffering, and delivery pipeline
