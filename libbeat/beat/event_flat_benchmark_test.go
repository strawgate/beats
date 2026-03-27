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

package beat

import (
	"testing"
	"time"

	"github.com/elastic/elastic-agent-libs/mapstr"
)

var (
	flatBenchShared = []mapstr.M{
		{"elastic_agent": mapstr.M{"id": "agent-uuid", "snapshot": false, "version": "8.12.0"}},
		{"agent": mapstr.M{"id": "agent-uuid"}},
		{"data_stream": mapstr.M{"type": "logs", "dataset": "system.syslog", "namespace": "default"}},
		{"event": mapstr.M{"dataset": "system.syslog"}},
		{"cloud": mapstr.M{
			"provider": "aws", "region": "us-east-1", "availability_zone": "us-east-1a",
			"account": mapstr.M{"id": "123456789012"},
			"instance": mapstr.M{"id": "i-0abcdef"},
			"machine": mapstr.M{"type": "m5.xlarge"},
			"service": mapstr.M{"name": "EC2"},
		}},
	}
	flatBenchBuiltin = mapstr.M{
		"ecs":   mapstr.M{"version": "8.0.0"},
		"host":  mapstr.M{"name": "server1"},
		"agent": mapstr.M{"type": "metricbeat", "version": "8.12.0"},
	}

	flatBenchSink interface{}
)

func newFlatBenchEvent() *Event {
	return &Event{
		Timestamp: time.Now(),
		Fields: mapstr.M{
			"message": "test log message",
			"agent":   mapstr.M{"type": "filebeat"},
		},
	}
}

func BenchmarkPipelineNested(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := newFlatBenchEvent()
		for _, s := range flatBenchShared {
			e.DeepUpdate(s.Clone())
		}
		e.DeepUpdateNoOverwrite(flatBenchBuiltin.Clone())
		// rename
		v, _ := e.GetValue("message")
		_ = e.Delete("message")
		_, _ = e.PutValue("event.original", v)
		flatBenchSink = e.Fields
	}
}

func BenchmarkPipelineFlat(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := newFlatBenchEvent()
		e.EnableFlat()
		for _, s := range flatBenchShared {
			e.DeepUpdate(s)
		}
		e.DeepUpdateNoOverwrite(flatBenchBuiltin)
		// rename
		v, _ := e.GetValue("message")
		_ = e.Delete("message")
		_, _ = e.PutValue("event.original", v)
		e.Render()
		flatBenchSink = e.Fields
	}
}

func BenchmarkPipelineFlatNoRender(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := newFlatBenchEvent()
		e.EnableFlat()
		for _, s := range flatBenchShared {
			e.DeepUpdate(s)
		}
		e.DeepUpdateNoOverwrite(flatBenchBuiltin)
		// rename
		v, _ := e.GetValue("message")
		_ = e.Delete("message")
		_, _ = e.PutValue("event.original", v)
		flatBenchSink = e
	}
}
