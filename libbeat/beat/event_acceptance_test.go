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

// Event Acceptance Tests
//
// These tests define the exact behavioral contract of beat.Event that any
// alternative storage implementation (e.g. FlatMap) must satisfy. They test
// the public API (GetValue, PutValue, Delete, HasKey, DeepUpdate, Clone)
// without depending on the internal storage representation.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/mapstr"
)

// --- GetValue ---

func TestAcceptanceGetValueScalar(t *testing.T) {
	e := &Event{Fields: mapstr.M{"message": "hello"}}

	v, err := e.GetValue("message")
	require.NoError(t, err)
	assert.Equal(t, "hello", v)
}

func TestAcceptanceGetValueNested(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"cloud": mapstr.M{"provider": "aws", "region": "us-east-1"},
	}}

	v, err := e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "aws", v)
}

func TestAcceptanceGetValueDeepNested(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"cloud": mapstr.M{"account": mapstr.M{"id": "123"}},
	}}

	v, err := e.GetValue("cloud.account.id")
	require.NoError(t, err)
	assert.Equal(t, "123", v)
}

func TestAcceptanceGetValueParentReturnsMap(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"cloud": mapstr.M{"provider": "aws", "region": "us-east-1"},
	}}

	v, err := e.GetValue("cloud")
	require.NoError(t, err)
	m, ok := v.(mapstr.M)
	require.True(t, ok)
	assert.Equal(t, "aws", m["provider"])
	assert.Equal(t, "us-east-1", m["region"])
}

func TestAcceptanceGetValueMissing(t *testing.T) {
	e := &Event{Fields: mapstr.M{"a": "b"}}
	_, err := e.GetValue("missing")
	assert.Error(t, err)
}

func TestAcceptanceGetValueTimestamp(t *testing.T) {
	now := time.Now()
	e := &Event{Timestamp: now, Fields: mapstr.M{}}

	v, err := e.GetValue("@timestamp")
	require.NoError(t, err)
	assert.Equal(t, now, v)
}

func TestAcceptanceGetValueMetadata(t *testing.T) {
	e := &Event{Meta: mapstr.M{"pipeline": "test"}, Fields: mapstr.M{}}

	v, err := e.GetValue("@metadata.pipeline")
	require.NoError(t, err)
	assert.Equal(t, "test", v)
}

func TestAcceptanceGetValueMetadataKeyDirectly(t *testing.T) {
	e := &Event{Meta: mapstr.M{"pipeline": "test"}, Fields: mapstr.M{}}

	_, err := e.GetValue("@metadata")
	assert.Error(t, err, "direct @metadata access should return error")
}

func TestAcceptanceGetValueNilFields(t *testing.T) {
	e := &Event{}
	_, err := e.GetValue("anything")
	assert.Error(t, err)
}

// --- PutValue ---

func TestAcceptancePutValueScalar(t *testing.T) {
	e := &Event{Fields: mapstr.M{}}

	_, err := e.PutValue("message", "hello")
	require.NoError(t, err)

	v, err := e.GetValue("message")
	require.NoError(t, err)
	assert.Equal(t, "hello", v)
}

func TestAcceptancePutValueNested(t *testing.T) {
	e := &Event{Fields: mapstr.M{}}

	_, err := e.PutValue("cloud.provider", "aws")
	require.NoError(t, err)

	v, err := e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "aws", v)
}

func TestAcceptancePutValueOverwrite(t *testing.T) {
	e := &Event{Fields: mapstr.M{"key": "old"}}

	old, err := e.PutValue("key", "new")
	require.NoError(t, err)
	assert.Equal(t, "old", old)

	v, _ := e.GetValue("key")
	assert.Equal(t, "new", v)
}

func TestAcceptancePutValuePreservesSiblings(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"cloud": mapstr.M{"provider": "aws"},
	}}

	_, err := e.PutValue("cloud.region", "us-east-1")
	require.NoError(t, err)

	v, _ := e.GetValue("cloud.provider")
	assert.Equal(t, "aws", v)
	v, _ = e.GetValue("cloud.region")
	assert.Equal(t, "us-east-1", v)
}

func TestAcceptancePutValueTimestamp(t *testing.T) {
	now := time.Now()
	e := &Event{Fields: mapstr.M{}}

	_, err := e.PutValue("@timestamp", now)
	require.NoError(t, err)
	assert.Equal(t, now, e.Timestamp)
}

func TestAcceptancePutValueMetadata(t *testing.T) {
	e := &Event{Fields: mapstr.M{}}

	_, err := e.PutValue("@metadata.pipeline", "test")
	require.NoError(t, err)

	v, _ := e.GetValue("@metadata.pipeline")
	assert.Equal(t, "test", v)
}

func TestAcceptancePutValueNilFields(t *testing.T) {
	e := &Event{}
	_, err := e.PutValue("key", "value")
	require.NoError(t, err)

	v, _ := e.GetValue("key")
	assert.Equal(t, "value", v)
}

func TestAcceptancePutValueMap(t *testing.T) {
	e := &Event{Fields: mapstr.M{}}

	_, err := e.PutValue("cloud", mapstr.M{"provider": "aws", "region": "us-east-1"})
	require.NoError(t, err)

	v, _ := e.GetValue("cloud.provider")
	assert.Equal(t, "aws", v)
	v, _ = e.GetValue("cloud.region")
	assert.Equal(t, "us-east-1", v)
}

// --- Delete ---

func TestAcceptanceDeleteScalar(t *testing.T) {
	e := &Event{Fields: mapstr.M{"a": "1", "b": "2"}}

	err := e.Delete("a")
	require.NoError(t, err)

	_, err = e.GetValue("a")
	assert.Error(t, err)

	v, _ := e.GetValue("b")
	assert.Equal(t, "2", v)
}

func TestAcceptanceDeleteNested(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"cloud": mapstr.M{"provider": "aws", "region": "us-east-1"},
	}}

	err := e.Delete("cloud.provider")
	require.NoError(t, err)

	_, err = e.GetValue("cloud.provider")
	assert.Error(t, err)

	v, _ := e.GetValue("cloud.region")
	assert.Equal(t, "us-east-1", v)
}

func TestAcceptanceDeleteMissing(t *testing.T) {
	e := &Event{Fields: mapstr.M{"a": "b"}}
	err := e.Delete("missing")
	assert.Error(t, err)
}

func TestAcceptanceDeleteTimestamp(t *testing.T) {
	e := &Event{Fields: mapstr.M{}}
	err := e.Delete("@timestamp")
	assert.Error(t, err)
}

func TestAcceptanceDeleteMetadata(t *testing.T) {
	e := &Event{Meta: mapstr.M{"pipeline": "test"}, Fields: mapstr.M{}}

	err := e.Delete("@metadata.pipeline")
	require.NoError(t, err)

	_, err = e.GetValue("@metadata.pipeline")
	assert.Error(t, err)
}

// --- HasKey ---

func TestAcceptanceHasKey(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"cloud": mapstr.M{"provider": "aws"},
	}}

	ok, _ := e.HasKey("cloud.provider")
	assert.True(t, ok)

	ok, _ = e.HasKey("cloud")
	assert.True(t, ok)

	ok, _ = e.HasKey("missing")
	assert.False(t, ok)
}

// --- DeepUpdate ---

func TestAcceptanceDeepUpdateMerge(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"agent": mapstr.M{"type": "filebeat"},
	}}

	e.DeepUpdate(mapstr.M{
		"agent": mapstr.M{"id": "agent-123"},
	})

	v, _ := e.GetValue("agent.type")
	assert.Equal(t, "filebeat", v)
	v, _ = e.GetValue("agent.id")
	assert.Equal(t, "agent-123", v)
}

func TestAcceptanceDeepUpdateOverwrite(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"agent": mapstr.M{"type": "filebeat"},
	}}

	e.DeepUpdate(mapstr.M{
		"agent": mapstr.M{"type": "metricbeat"},
	})

	v, _ := e.GetValue("agent.type")
	assert.Equal(t, "metricbeat", v)
}

func TestAcceptanceDeepUpdateNoOverwrite(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"agent": mapstr.M{"type": "filebeat"},
	}}

	e.DeepUpdateNoOverwrite(mapstr.M{
		"agent": mapstr.M{"type": "metricbeat", "id": "agent-123"},
	})

	v, _ := e.GetValue("agent.type")
	assert.Equal(t, "filebeat", v, "existing value must not be overwritten")
	v, _ = e.GetValue("agent.id")
	assert.Equal(t, "agent-123", v, "new value must be added")
}

func TestAcceptanceDeepUpdateTimestamp(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	e := &Event{Timestamp: now, Fields: mapstr.M{}}

	e.DeepUpdate(mapstr.M{
		"@timestamp": later,
		"message":    "hello",
	})

	assert.Equal(t, later, e.Timestamp)
	v, _ := e.GetValue("message")
	assert.Equal(t, "hello", v)
}

func TestAcceptanceDeepUpdateMetadata(t *testing.T) {
	e := &Event{Fields: mapstr.M{}}

	e.DeepUpdate(mapstr.M{
		"@metadata": mapstr.M{"pipeline": "test"},
		"message":   "hello",
	})

	v, _ := e.GetValue("@metadata.pipeline")
	assert.Equal(t, "test", v)
	v, _ = e.GetValue("message")
	assert.Equal(t, "hello", v)
}

func TestAcceptanceDeepUpdateInputMapNotMutated(t *testing.T) {
	ts := time.Now()
	update := mapstr.M{
		TimestampFieldKey: ts,
		MetadataFieldKey:  mapstr.M{"key": "value"},
		"regular":         "field",
	}
	updateCopy := update.Clone()

	e := &Event{Fields: mapstr.M{}}
	e.DeepUpdate(update)

	assert.Equal(t, updateCopy, update, "input map must not be mutated by DeepUpdate")
}

// --- Clone ---

func TestAcceptanceCloneIndependence(t *testing.T) {
	e := &Event{
		Timestamp: time.Now(),
		Meta:      mapstr.M{"pipeline": "test"},
		Fields: mapstr.M{
			"message": "hello",
			"cloud":   mapstr.M{"provider": "aws"},
		},
	}

	c := e.Clone()

	// Mutate clone.
	_, _ = c.PutValue("cloud.provider", "MUTATED")
	_, _ = c.PutValue("message", "MUTATED")
	c.Meta["pipeline"] = "MUTATED"

	// Original must be unaffected.
	v, _ := e.GetValue("cloud.provider")
	assert.Equal(t, "aws", v)
	v, _ = e.GetValue("message")
	assert.Equal(t, "hello", v)
	assert.Equal(t, "test", e.Meta["pipeline"])
}

// --- Processor pipeline simulation ---

func TestAcceptanceProcessorChain(t *testing.T) {
	e := &Event{
		Timestamp: time.Now(),
		Fields: mapstr.M{
			"message": "test log",
			"agent":   mapstr.M{"type": "filebeat"},
		},
	}

	// addFields: elastic_agent (overwrite=true, shared)
	e.DeepUpdate(mapstr.M{
		"elastic_agent": mapstr.M{"id": "agent-123", "version": "8.12.0"},
	})

	// addFields: agent.id (overwrite=true, shared)
	e.DeepUpdate(mapstr.M{
		"agent": mapstr.M{"id": "agent-123"},
	})

	// addFields: @metadata (overwrite=true, shared)
	e.DeepUpdate(mapstr.M{
		"@metadata": mapstr.M{"input_id": "logfile-system"},
	})

	// addFields: data_stream (overwrite=true, shared)
	e.DeepUpdate(mapstr.M{
		"data_stream": mapstr.M{"type": "logs", "dataset": "system.syslog"},
	})

	// addFields: builtin meta (overwrite=false)
	e.DeepUpdateNoOverwrite(mapstr.M{
		"ecs":   mapstr.M{"version": "8.0.0"},
		"host":  mapstr.M{"name": "server1"},
		"agent": mapstr.M{"type": "metricbeat", "version": "8.12.0"},
	})

	// Verify all fields.
	v, _ := e.GetValue("elastic_agent.id")
	assert.Equal(t, "agent-123", v)

	v, _ = e.GetValue("agent.id")
	assert.Equal(t, "agent-123", v)

	// agent.type should be "filebeat" (no-overwrite preserved it)
	v, _ = e.GetValue("agent.type")
	assert.Equal(t, "filebeat", v)

	// agent.version added by no-overwrite
	v, _ = e.GetValue("agent.version")
	assert.Equal(t, "8.12.0", v)

	v, _ = e.GetValue("@metadata.input_id")
	assert.Equal(t, "logfile-system", v)

	v, _ = e.GetValue("data_stream.type")
	assert.Equal(t, "logs", v)

	v, _ = e.GetValue("ecs.version")
	assert.Equal(t, "8.0.0", v)

	v, _ = e.GetValue("message")
	assert.Equal(t, "test log", v)
}

func TestAcceptanceRenameSimulation(t *testing.T) {
	e := &Event{Fields: mapstr.M{
		"message": "hello",
		"source":  "stdin",
	}}

	// Rename: source → input.source
	v, _ := e.GetValue("source")
	_ = e.Delete("source")
	_, _ = e.PutValue("input.source", v)

	_, err := e.GetValue("source")
	assert.Error(t, err)

	v, _ = e.GetValue("input.source")
	assert.Equal(t, "stdin", v)

	v, _ = e.GetValue("message")
	assert.Equal(t, "hello", v)
}
