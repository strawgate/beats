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

package processors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/processors/actions/addfields"
	"github.com/elastic/beats/v7/libbeat/processors/add_cloud_metadata"
	conf "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/logp/logptest"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

var benchSink interface{}

func newBenchEvent() *beat.Event {
	return &beat.Event{
		Timestamp: time.Now(),
		Fields: mapstr.M{
			"message": "Mar 25 10:00:00 server sshd[12345]: Accepted publickey for user",
		},
	}
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

// makeCloudProcessor creates an add_cloud_metadata processor with pre-seeded
// metadata from a local test server, bypassing real cloud API calls.
func makeCloudProcessor(b *testing.B) beat.Processor {
	b.Helper()
	_ = logp.TestingSetup() //nolint:staticcheck

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.RequestURI {
		case "/2009-04-04/meta-data/instance-id":
			_, _ = w.Write([]byte("i-0abcdef1234567890"))
		case "/2009-04-04/meta-data/instance-type":
			_, _ = w.Write([]byte("m5.xlarge"))
		case "/2009-04-04/meta-data/hostname":
			_, _ = w.Write([]byte("ip-172-31-0-1.ec2.internal"))
		case "/2009-04-04/meta-data/placement/availability-zone":
			_, _ = w.Write([]byte("us-east-1a"))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	b.Cleanup(server.Close)

	config, err := conf.NewConfigFrom(map[string]interface{}{
		"providers": []string{"openstack"},
		"host":      server.Listener.Addr().String(),
	})
	if err != nil {
		b.Fatal(err)
	}

	p, err := add_cloud_metadata.New(config, logptest.NewTestingLogger(b, ""))
	if err != nil {
		b.Fatal(err)
	}

	// Warm up: run once to trigger init and metadata fetch.
	warmup := &beat.Event{Fields: mapstr.M{}}
	_, _ = p.Run(warmup)

	return p
}

// BenchmarkFullPipelineWithCloudMetadata benchmarks a realistic processor chain:
// builtin fields (no-overwrite) + agent add_fields chain + cloud metadata + rename.
func BenchmarkFullPipelineWithCloudMetadata(b *testing.B) {
	cloudProc := makeCloudProcessor(b)

	builtinMeta := addfields.NewAddFields(mapstr.M{
		"ecs":   mapstr.M{"version": "8.0.0"},
		"host":  mapstr.M{"name": "prod-server-01"},
		"agent": mapstr.M{"ephemeral_id": "ephemeral-123", "id": "agent-uuid", "name": "prod-server-01", "type": "filebeat", "version": "8.12.0"},
	}, true, false)

	agentProcs := []beat.Processor{
		addfields.NewAddFields(mapstr.M{
			"elastic_agent": mapstr.M{"id": "agent-uuid", "snapshot": false, "version": "8.12.0"},
		}, true, true),
		addfields.NewAddFields(mapstr.M{
			"agent": mapstr.M{"id": "agent-uuid"},
		}, true, true),
		addfields.NewAddFields(mapstr.M{
			"@metadata": mapstr.M{"input_id": "logfile-system-default"},
		}, true, true),
		addfields.NewAddFields(mapstr.M{
			"data_stream": mapstr.M{"type": "logs", "dataset": "system.syslog", "namespace": "default"},
		}, true, true),
		addfields.NewAddFields(mapstr.M{
			"event": mapstr.M{"dataset": "system.syslog"},
		}, true, true),
		addfields.NewAddFields(mapstr.M{
			"@metadata": mapstr.M{"stream_id": "stream-uuid-5678"},
		}, true, true),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := newBenchEvent()
		e, _ = builtinMeta.Run(e)
		for _, p := range agentProcs {
			e, _ = p.Run(e)
		}
		e, _ = cloudProc.Run(e)
		e, _ = (&renameProc{}).Run(e)
		e.Materialize()
		benchSink = e.Fields
	}
}
