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

// Shared processor data — same structure as real elastic agent processors.
var (
	benchElasticAgent = mapstr.M{"id": "agent-uuid", "snapshot": false, "version": "8.12.0"}
	benchAgent        = mapstr.M{"id": "agent-uuid"}
	benchDataStream   = mapstr.M{"type": "logs", "dataset": "system.syslog", "namespace": "default"}
	benchEventDataset = mapstr.M{"dataset": "system.syslog"}
	benchCloud        = mapstr.M{
		"provider": "aws", "region": "us-east-1", "availability_zone": "us-east-1a",
		"account":  mapstr.M{"id": "123456789012"},
		"instance": mapstr.M{"id": "i-0abcdef"},
		"machine":  mapstr.M{"type": "m5.xlarge"},
		"service":  mapstr.M{"name": "EC2"},
	}
	benchHost = mapstr.M{
		"name":         "server1",
		"hostname":     "server1.example.com",
		"architecture": "x86_64",
		"os":           mapstr.M{"family": "linux", "name": "Ubuntu", "version": "22.04"},
	}
	benchBuiltin = mapstr.M{
		"ecs":   mapstr.M{"version": "8.0.0"},
		"host":  mapstr.M{"name": "server1"},
		"agent": mapstr.M{"type": "metricbeat", "version": "8.12.0"},
	}

	benchSink interface{}
)

func newPipelineEvent() *beat.Event {
	e := &beat.Event{
		Timestamp: time.Now(),
	}
	e.SetFields(mapstr.M{
		"message": "test log message",
	})
	return e
}

// renameProc simulates a rename processor (message -> event.original).
type renameProc struct{}

func (p *renameProc) Run(event *beat.Event) (*beat.Event, error) {
	v, _ := event.GetValue("message")
	_ = event.Delete("message")
	_, _ = event.PutValue("event.original", v)
	return event, nil
}

func (p *renameProc) String() string { return "rename" }

// BenchmarkRealProcessorPipeline uses real addfields processors with cowMap.
// Production order: agent-injected processors first, then builtin (no-overwrite).
func BenchmarkRealProcessorPipeline(b *testing.B) {
	// Agent-injected processors run first (prepended by elastic agent).
	agentProcs := []beat.Processor{
		MakeFieldsProcessor("elastic_agent", benchElasticAgent, true),
		MakeFieldsProcessor("agent", benchAgent, true),
		MakeFieldsProcessor("data_stream", benchDataStream, true),
		MakeFieldsProcessor("event", benchEventDataset, true),
		MakeFieldsProcessor("cloud", benchCloud, true),
		MakeFieldsProcessor("host", benchHost, true),
	}
	// Builtin (no-overwrite) runs after agent processors.
	builtinMeta := NewAddFields(benchBuiltin, true, false)
	rename := &renameProc{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := newPipelineEvent()
		for _, p := range agentProcs {
			e, _ = p.Run(e)
		}
		e, _ = builtinMeta.Run(e)
		e, _ = rename.Run(e)
		benchSink = e.Fields
	}
}

// BenchmarkRealProcessorPipelineMaterialize same but includes Materialize.
func BenchmarkRealProcessorPipelineMaterialize(b *testing.B) {
	agentProcs := []beat.Processor{
		MakeFieldsProcessor("elastic_agent", benchElasticAgent, true),
		MakeFieldsProcessor("agent", benchAgent, true),
		MakeFieldsProcessor("data_stream", benchDataStream, true),
		MakeFieldsProcessor("event", benchEventDataset, true),
		MakeFieldsProcessor("cloud", benchCloud, true),
		MakeFieldsProcessor("host", benchHost, true),
	}
	builtinMeta := NewAddFields(benchBuiltin, true, false)
	rename := &renameProc{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := newPipelineEvent()
		for _, p := range agentProcs {
			e, _ = p.Run(e)
		}
		e, _ = builtinMeta.Run(e)
		e, _ = rename.Run(e)
		e.Materialize()
		benchSink = e.Fields
	}
}

// BenchmarkRealProcessorPipelinePooled uses the event pool.
func BenchmarkRealProcessorPipelinePooled(b *testing.B) {
	agentProcs := []beat.Processor{
		MakeFieldsProcessor("elastic_agent", benchElasticAgent, true),
		MakeFieldsProcessor("agent", benchAgent, true),
		MakeFieldsProcessor("data_stream", benchDataStream, true),
		MakeFieldsProcessor("event", benchEventDataset, true),
		MakeFieldsProcessor("cloud", benchCloud, true),
		MakeFieldsProcessor("host", benchHost, true),
	}
	builtinMeta := NewAddFields(benchBuiltin, true, false)
	rename := &renameProc{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := beat.NewEvent()
		e.Timestamp = time.Now()
		_ = e.PutValueQuiet("message", "test log message")
		for _, p := range agentProcs {
			e, _ = p.Run(e)
		}
		e, _ = builtinMeta.Run(e)
		e, _ = rename.Run(e)
		benchSink = e.FieldsUnsafe()
		beat.ReleaseEvent(e)
	}
}
