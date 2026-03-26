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

package processingbench

import (
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/publisher/processing"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent-libs/paths"
)

// BenchmarkProcessingPipeline benchmarks the full default beat processing
// pipeline including normalization, builtin metadata (ecs.version, host.name,
// agent.*), and field merging via addFields processors. This measures the
// real per-event cost that every event pays in a standard beat configuration.
func BenchmarkProcessingPipeline(b *testing.B) {
	_ = logp.DevelopmentSetup(logp.WithLevel(logp.InfoLevel)) //nolint:staticcheck // global logger needed for processing pipeline

	info := beat.Info{
		Beat:        "testbeat",
		Version:     "8.12.0",
		Name:        "test-host",
		Hostname:    "test-host",
		ID:          uuid.Must(uuid.NewV4()),
		EphemeralID: uuid.Must(uuid.NewV4()),
	}

	s, err := processing.MakeDefaultBeatSupport(true)(info, logp.L(), config.NewConfig())
	require.NoError(b, err)

	prog, err := s.Create(beat.ProcessingConfig{}, false, &paths.Path{})
	require.NoError(b, err)

	fields := mapstr.M{"message": "test log event", "log": mapstr.M{"level": "info"}}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f := fields.Clone()
		_, _ = prog.Run(&beat.Event{Fields: f})
	}
}

// BenchmarkProcessingPipelineMinimal benchmarks the processing pipeline
// with a minimal event (single field), matching BenchmarkNormalization.
func BenchmarkProcessingPipelineMinimal(b *testing.B) {
	_ = logp.DevelopmentSetup(logp.WithLevel(logp.InfoLevel)) //nolint:staticcheck // global logger needed for processing pipeline

	info := beat.Info{
		Beat:        "testbeat",
		Version:     "8.12.0",
		Name:        "test-host",
		Hostname:    "test-host",
		ID:          uuid.Must(uuid.NewV4()),
		EphemeralID: uuid.Must(uuid.NewV4()),
	}

	s, err := processing.MakeDefaultBeatSupport(true)(info, logp.L(), config.NewConfig())
	require.NoError(b, err)

	prog, err := s.Create(beat.ProcessingConfig{}, false, &paths.Path{})
	require.NoError(b, err)

	fields := mapstr.M{"a": "b"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f := fields.Clone()
		_, _ = prog.Run(&beat.Event{Fields: f})
	}
}
