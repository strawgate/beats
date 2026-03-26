// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package addfields

import (
	"testing"
	"time"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

// makeEvent creates a realistic event similar to what Elastic Agent produces.
func makeEvent() *beat.Event {
	return &beat.Event{
		Timestamp: time.Now(),
		Fields: mapstr.M{
			"message": "test log message",
			"log": mapstr.M{
				"level": "info",
				"file": mapstr.M{
					"path": "/var/log/app.log",
					"line": 42,
				},
			},
			"input": mapstr.M{
				"type": "filestream",
			},
		},
	}
}

// BenchmarkAddFields_AgentMetadata benchmarks the typical Elastic Agent pattern
// of adding agent metadata (agent.name, agent.version, etc.) to every event.
// This is the single hottest use of add_fields in production.
func BenchmarkAddFields_AgentMetadata(b *testing.B) {
	// This mirrors WithAgentMeta() in libbeat/publisher/processing/default.go
	agentFields := mapstr.M{
		"agent": mapstr.M{
			"ephemeral_id": "b0e83d28-8d3f-4b2a-9e1a-4c5d6e7f8a9b",
			"id":           "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"name":         "my-hostname",
			"type":         "filebeat",
			"version":      "8.12.0",
		},
	}

	b.Run("shared=true", func(b *testing.B) {
		proc := NewAddFields(agentFields, true, true)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := makeEvent()
			_, _ = proc.Run(event)
		}
	})

	b.Run("shared=false", func(b *testing.B) {
		proc := NewAddFields(agentFields, false, true)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := makeEvent()
			_, _ = proc.Run(event)
		}
	})
}

// BenchmarkAddFields_ECSVersion benchmarks adding the simple ecs.version field.
func BenchmarkAddFields_ECSVersion(b *testing.B) {
	ecsFields := mapstr.M{
		"ecs": mapstr.M{
			"version": "8.0.0",
		},
	}

	proc := NewAddFields(ecsFields, true, true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := makeEvent()
		_, _ = proc.Run(event)
	}
}

// BenchmarkAddFields_HostName benchmarks the host.name builtin field.
func BenchmarkAddFields_HostName(b *testing.B) {
	hostFields := mapstr.M{
		"host": mapstr.M{
			"name": "my-hostname",
		},
	}

	proc := NewAddFields(hostFields, true, true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := makeEvent()
		_, _ = proc.Run(event)
	}
}

// BenchmarkAddFields_CombinedBuiltins benchmarks the combined effect of all
// builtin metadata that Elastic Agent adds: agent + ecs + host fields.
// This represents the realistic per-event overhead.
func BenchmarkAddFields_CombinedBuiltins(b *testing.B) {
	builtinFields := mapstr.M{
		"agent": mapstr.M{
			"ephemeral_id": "b0e83d28-8d3f-4b2a-9e1a-4c5d6e7f8a9b",
			"id":           "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"name":         "my-hostname",
			"type":         "filebeat",
			"version":      "8.12.0",
		},
		"ecs": mapstr.M{
			"version": "8.0.0",
		},
		"host": mapstr.M{
			"name": "my-hostname",
		},
	}

	b.Run("shared=true", func(b *testing.B) {
		proc := NewAddFields(builtinFields, true, true)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := makeEvent()
			_, _ = proc.Run(event)
		}
	})

	b.Run("shared=false", func(b *testing.B) {
		proc := NewAddFields(builtinFields, false, true)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := makeEvent()
			_, _ = proc.Run(event)
		}
	})
}

// BenchmarkAddFields_FlatScalars benchmarks adding simple flat scalar fields
// under the default "fields" target - a common user configuration.
func BenchmarkAddFields_FlatScalars(b *testing.B) {
	fields := mapstr.M{
		"fields": mapstr.M{
			"environment": "production",
			"datacenter":  "us-east-1",
			"team":        "platform",
		},
	}

	proc := NewAddFields(fields, true, true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := makeEvent()
		_, _ = proc.Run(event)
	}
}

// BenchmarkAddFields_Metadata benchmarks adding @metadata fields,
// used by outputs for routing decisions.
func BenchmarkAddFields_Metadata(b *testing.B) {
	metaFields := mapstr.M{
		"@metadata": mapstr.M{
			"_id":     "doc-123",
			"op_type": "index",
			"pipeline": "my-pipeline",
		},
	}

	proc := NewAddFields(metaFields, true, true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := makeEvent()
		_, _ = proc.Run(event)
	}
}

// BenchmarkAddFields_NoOverwrite benchmarks the overwrite=false path,
// used for builtin metadata that should not override user-set fields.
func BenchmarkAddFields_NoOverwrite(b *testing.B) {
	fields := mapstr.M{
		"agent": mapstr.M{
			"name":    "default-host",
			"version": "8.12.0",
		},
	}

	b.Run("no_conflict", func(b *testing.B) {
		proc := NewAddFields(fields, true, false)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := makeEvent()
			_, _ = proc.Run(event)
		}
	})

	b.Run("with_conflict", func(b *testing.B) {
		proc := NewAddFields(fields, true, false)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := makeEvent()
			event.Fields["agent"] = mapstr.M{"name": "custom-host"}
			_, _ = proc.Run(event)
		}
	})
}

// BenchmarkAddFields_LargeFieldSet benchmarks adding many fields at once,
// simulating a heavily-enriched pipeline.
func BenchmarkAddFields_LargeFieldSet(b *testing.B) {
	fields := mapstr.M{
		"cloud": mapstr.M{
			"provider":          "aws",
			"region":            "us-east-1",
			"availability_zone": "us-east-1a",
			"account": mapstr.M{
				"id":   "123456789",
				"name": "production",
			},
			"instance": mapstr.M{
				"id":   "i-1234567890abcdef0",
				"name": "web-server-01",
			},
			"machine": mapstr.M{
				"type": "t3.large",
			},
		},
		"orchestrator": mapstr.M{
			"cluster": mapstr.M{
				"name": "prod-cluster",
				"url":  "https://k8s.example.com",
			},
			"namespace": "default",
			"resource": mapstr.M{
				"type": "pod",
				"name": "my-app-7b9f5c6d4f-x2k9l",
			},
		},
	}

	proc := NewAddFields(fields, true, true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := makeEvent()
		_, _ = proc.Run(event)
	}
}

// BenchmarkAddFields_ProcessorChain benchmarks a realistic chain of
// add_fields processors as configured by Elastic Agent.
func BenchmarkAddFields_ProcessorChain(b *testing.B) {
	// Processor 1: ECS version
	ecsProc := NewAddFields(mapstr.M{
		"ecs": mapstr.M{"version": "8.0.0"},
	}, true, true)

	// Processor 2: Host metadata
	hostProc := NewAddFields(mapstr.M{
		"host": mapstr.M{"name": "my-hostname"},
	}, true, true)

	// Processor 3: Agent metadata
	agentProc := NewAddFields(mapstr.M{
		"agent": mapstr.M{
			"ephemeral_id": "b0e83d28-8d3f-4b2a-9e1a-4c5d6e7f8a9b",
			"id":           "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"name":         "my-hostname",
			"type":         "filebeat",
			"version":      "8.12.0",
		},
	}, true, true)

	// Processor 4: Custom fields (user config)
	customProc := NewAddFields(mapstr.M{
		"fields": mapstr.M{
			"environment": "production",
			"team":        "platform",
		},
	}, true, true)

	// Processor 5: Builtin meta (overwrite=false)
	builtinProc := NewAddFields(mapstr.M{
		"agent": mapstr.M{
			"name":    "default",
			"version": "8.12.0",
		},
	}, true, false)

	procs := []beat.Processor{ecsProc, hostProc, agentProc, customProc, builtinProc}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := makeEvent()
		var err error
		for _, p := range procs {
			event, err = p.Run(event)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkCloneOverhead isolates the cost of Clone() that shared=true forces.
func BenchmarkCloneOverhead(b *testing.B) {
	// Small: just agent metadata
	small := mapstr.M{
		"agent": mapstr.M{
			"name":    "my-hostname",
			"version": "8.12.0",
		},
	}

	// Medium: agent + ecs + host
	medium := mapstr.M{
		"agent": mapstr.M{
			"ephemeral_id": "b0e83d28-8d3f-4b2a-9e1a-4c5d6e7f8a9b",
			"id":           "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"name":         "my-hostname",
			"type":         "filebeat",
			"version":      "8.12.0",
		},
		"ecs":  mapstr.M{"version": "8.0.0"},
		"host": mapstr.M{"name": "my-hostname"},
	}

	// Large: deeply nested cloud + k8s metadata
	large := mapstr.M{
		"cloud": mapstr.M{
			"provider": "aws",
			"region":   "us-east-1",
			"account":  mapstr.M{"id": "123", "name": "prod"},
			"instance": mapstr.M{"id": "i-abc", "name": "web-01"},
			"machine":  mapstr.M{"type": "t3.large"},
		},
		"orchestrator": mapstr.M{
			"cluster":   mapstr.M{"name": "prod", "url": "https://k8s.example.com"},
			"namespace": "default",
			"resource":  mapstr.M{"type": "pod", "name": "my-app-xyz"},
		},
		"agent": mapstr.M{
			"ephemeral_id": "b0e83d28-8d3f-4b2a-9e1a-4c5d6e7f8a9b",
			"id":           "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"name":         "my-hostname",
			"type":         "filebeat",
			"version":      "8.12.0",
		},
	}

	for name, fields := range map[string]mapstr.M{
		"small":  small,
		"medium": medium,
		"large":  large,
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = fields.Clone()
			}
		})
	}
}
