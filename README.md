[![Go Report Card](https://goreportcard.com/badge/github.com/elastic/beats)](https://goreportcard.com/report/github.com/elastic/beats)
[![Reviewed by Hound](https://img.shields.io/badge/Reviewed_by-Hound-8E64B0.svg)](https://houndci.com)

# Beats - The Lightweight Shippers of the Elastic Stack

The [Beats](https://www.elastic.co/beats) are lightweight data
shippers, written in Go, that you install on your servers to capture all sorts
of operational data (think of logs, metrics, or network packet data). The Beats
send the operational data to Elasticsearch, either directly or via Logstash, so
it can be visualized with Kibana.

By "lightweight", we mean that Beats have a small installation footprint, use
limited system resources, and have no runtime dependencies.

## Requirements

- Go 1.24.7+ for building from source
- Compatible with Elasticsearch 8.0+ and current 9.2.x series
- Supports Linux, macOS, and Windows platforms

This repository contains
[libbeat](https://github.com/elastic/beats/tree/main/libbeat), our Go
framework for creating Beats, and all the officially supported Beats:

Beat  | Description
--- | ---
[Auditbeat](https://github.com/elastic/beats/tree/main/auditbeat) | Collect your Linux audit framework data and monitor the integrity of your files with real-time file system monitoring.
[Filebeat](https://github.com/elastic/beats/tree/main/filebeat) | Tails and ships log files from various sources with built-in modules for common applications and systems.
[Heartbeat](https://github.com/elastic/beats/tree/main/heartbeat) | Ping remote services for availability and monitor uptime with detailed response metrics.
[Metricbeat](https://github.com/elastic/beats/tree/main/metricbeat) | Fetches sets of metrics from the operating system and services with extensive module support for monitoring infrastructure.
[Packetbeat](https://github.com/elastic/beats/tree/main/packetbeat) | Monitors the network and applications by sniffing packets and analyzing network traffic in real-time.
[Winlogbeat](https://github.com/elastic/beats/tree/main/winlogbeat) | Fetches and ships Windows Event logs with support for custom event log channels and filtering.
[Osquerybeat](https://github.com/elastic/beats/tree/main/x-pack/osquerybeat) | Runs Osquery and manages interaction with it for security monitoring and system introspection.

## Latest Features (9.2.x Series)

Recent enhancements in the current 9.2.x series include:

- **Azure Authentication**: Client secret authentication method for Azure Event Hub and storage in Filebeat
- **Transport Support**: AMQP-over-WebSocket transport support in processor v2
- **Cloud Integration**: Enhanced AWS S3 input with status reporting support
- **Kubernetes Metrics**: Updated container status metrics with `last_terminated_exitcode` field
- **Security Hardening**: Improved parsing resilience against malformed data and panic prevention
- **OpenTelemetry**: Updated dependencies for better observability integration

In addition to the above Beats, which are officially supported by
[Elastic](https://elastic.co), the community has created a set of other Beats
that make use of libbeat but live outside of this GitHub repository. We maintain
a list of community Beats
[here](https://www.elastic.co/guide/en/beats/libbeat/current/community-beats.html).

## Documentation and Getting Started

You can find the documentation and getting started guides for each of the Beats
on the [elastic.co site](https://www.elastic.co/guide/):

* [Beats platform](https://www.elastic.co/guide/en/beats/libbeat/current/index.html)
* [Auditbeat](https://www.elastic.co/guide/en/beats/auditbeat/current/index.html)
* [Filebeat](https://www.elastic.co/guide/en/beats/filebeat/current/index.html)
* [Heartbeat](https://www.elastic.co/guide/en/beats/heartbeat/current/index.html)
* [Metricbeat](https://www.elastic.co/guide/en/beats/metricbeat/current/index.html)
* [Packetbeat](https://www.elastic.co/guide/en/beats/packetbeat/current/index.html)
* [Winlogbeat](https://www.elastic.co/guide/en/beats/winlogbeat/current/index.html)

## Elastic Agent

The Elastic Agent is a single, unified agent that you can deploy to hosts or containers to collect data and send it to the Elastic Stack. Behind the scenes, Elastic Agent runs the Beats shippers or Elastic Endpoint required for your configuration.

You can find the documentation and getting started guides for the Elastic Agent
on the [elastic.co site](https://www.elastic.co/guide/en/fleet/current/elastic-agent-installation.html)

## Getting Help

If you need help or hit an issue, please start by opening a topic on our
[discuss forums](https://discuss.elastic.co/c/beats). Please note that we
reserve GitHub tickets for confirmed bugs and enhancement requests.

## Downloads

You can download pre-compiled Beats binaries, as well as packages for the
supported platforms, from [this page](https://www.elastic.co/downloads/beats).

## Contributing

We'd love working with you! You can help make the Beats better in many ways:
report issues, help us reproduce issues, fix bugs, add functionality, or even
create your own Beat.

Please start by reading our [CONTRIBUTING](CONTRIBUTING.md) file.

## Building Beats from the Source

See our [CONTRIBUTING](CONTRIBUTING.md) file for information about setting up
your dev environment to build Beats from the source.

## Snapshots

For testing purposes, we generate snapshot builds that you can find [here](https://artifacts-api.elastic.co/v1/search/9.0-SNAPSHOT/). Please be aware that these are built on top of main and are not meant for production.

## CI

### PR Comments

It is possible to trigger some jobs by putting a comment on a GitHub PR.
(This service is only available for users affiliated with Elastic and not for open-source contributors.)

* [beats CI pipeline](https://buildkite.com/elastic/beats)
  * `/test` will kick off a default build.
* [docs CI pipeline](https://buildkite.com/elastic/docs-build-pr)
  * `run docs-build` will kick off a docs build.

### PR Labels

It's possible to configure the build on a GitHub PR by labeling the PR with certain labels. Elastic users can find more details at https://docs.elastic.dev/ingest-dev-docs/beats/beats-ci.
