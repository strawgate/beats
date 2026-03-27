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

// Shared processor data — same structure as real elastic agent processors.
var (
	cowBenchSharedElasticAgent = mapstr.M{"id": "agent-uuid", "snapshot": false, "version": "8.12.0"}
	cowBenchSharedAgent        = mapstr.M{"id": "agent-uuid"}
	cowBenchSharedDataStream   = mapstr.M{"type": "logs", "dataset": "system.syslog", "namespace": "default"}
	cowBenchSharedEventDataset = mapstr.M{"dataset": "system.syslog"}
	cowBenchSharedCloud        = mapstr.M{
		"provider": "aws", "region": "us-east-1", "availability_zone": "us-east-1a",
		"account":  mapstr.M{"id": "123456789012"},
		"instance": mapstr.M{"id": "i-0abcdef"},
		"machine":  mapstr.M{"type": "m5.xlarge"},
		"service":  mapstr.M{"name": "EC2"},
	}
	cowBenchSharedHost = mapstr.M{
		"name":         "server1",
		"hostname":     "server1.example.com",
		"architecture": "x86_64",
		"os":           mapstr.M{"family": "linux", "name": "Ubuntu", "version": "22.04"},
	}
	cowBenchBuiltin = mapstr.M{
		"ecs":   mapstr.M{"version": "8.0.0"},
		"host":  mapstr.M{"name": "server1"},
		"agent": mapstr.M{"type": "metricbeat", "version": "8.12.0"},
	}

	cowBenchSink interface{}
)

func newBenchEvent() *Event {
	return &Event{
		Timestamp: time.Now(),
		Fields: mapstr.M{
			"message": "test log message",
			"agent":   mapstr.M{"type": "filebeat"},
		},
	}
}

// Real processor pipeline benchmarks are in
// libbeat/processors/actions/addfields/cowmap_pipeline_bench_test.go
// to avoid import cycles (beat -> addfields -> beat).

// BenchmarkCloneCowVsDeep compares Clone() cost with and without cowMaps.
func BenchmarkCloneCowVsDeep(b *testing.B) {
	b.Run("DeepClone", func(b *testing.B) {
		e := newBenchEvent()
		e.Fields["cloud"] = cowBenchSharedCloud.Clone()
		e.Fields["host"] = cowBenchSharedHost.Clone()
		e.Fields["elastic_agent"] = cowBenchSharedElasticAgent.Clone()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cowBenchSink = e.Clone()
		}
	})

	b.Run("CowClone", func(b *testing.B) {
		e := newBenchEvent()
		e.Fields["cloud"] = newCowMap(cowBenchSharedCloud)
		e.Fields["host"] = newCowMap(cowBenchSharedHost)
		e.Fields["elastic_agent"] = newCowMap(cowBenchSharedElasticAgent)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cowBenchSink = e.Clone()
		}
	})
}
