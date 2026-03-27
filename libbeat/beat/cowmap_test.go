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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/mapstr"
)

var sharedCloud = mapstr.M{
	"provider":          "aws",
	"region":            "us-east-1",
	"availability_zone": "us-east-1a",
	"account":           mapstr.M{"id": "123456789012"},
	"instance":          mapstr.M{"id": "i-0abcdef"},
	"machine":           mapstr.M{"type": "m5.xlarge"},
}

func newCowEvent() *Event {
	e := &Event{Timestamp: time.Now()}
	e.SetFields(mapstr.M{
		"message": "test log message",
		"agent":   mapstr.M{"type": "filebeat"},
	})
	_ = e.PutValueQuiet("cloud", newCowMap(sharedCloud))
	return e
}

func TestCowMapGetValueScalar(t *testing.T) {
	e := newCowEvent()
	v, err := e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "aws", v)
}

func TestCowMapGetValueNested(t *testing.T) {
	e := newCowEvent()
	v, err := e.GetValue("cloud.instance.id")
	require.NoError(t, err)
	assert.Equal(t, "i-0abcdef", v)
}

func TestCowMapGetValueEntireSubtree(t *testing.T) {
	e := newCowEvent()
	v, err := e.GetValue("cloud")
	require.NoError(t, err)
	// Returns a clone so callers can't corrupt shared data.
	assert.Equal(t, sharedCloud, v)

	// Mutating the returned map must not affect shared data.
	vMap := v.(mapstr.M)
	vMap["provider"] = "CORRUPTED"
	assert.Equal(t, "aws", sharedCloud["provider"])
}

func TestCowMapGetValueMissing(t *testing.T) {
	e := newCowEvent()
	_, err := e.GetValue("cloud.nonexistent")
	assert.Error(t, err)
}

func TestCowMapGetValueNonCowField(t *testing.T) {
	e := newCowEvent()
	v, err := e.GetValue("message")
	require.NoError(t, err)
	assert.Equal(t, "test log message", v)
}

func TestCowMapPutValueTriggersClone(t *testing.T) {
	e := newCowEvent()

	// Write into cowMap sub-tree — triggers copy-on-write.
	_, err := e.PutValue("cloud.instance.id", "i-modified")
	require.NoError(t, err)

	// Event sees the modified value.
	v, err := e.GetValue("cloud.instance.id")
	require.NoError(t, err)
	assert.Equal(t, "i-modified", v)

	// Shared data is NOT modified.
	assert.Equal(t, "i-0abcdef", sharedCloud["instance"].(mapstr.M)["id"])
}

func TestCowMapPutValueReplaceEntireSubtree(t *testing.T) {
	e := newCowEvent()

	newCloud := mapstr.M{"provider": "gcp", "region": "us-central1"}
	old, err := e.PutValue("cloud", newCloud)
	require.NoError(t, err)

	// Old value is a clone of shared data (PutValue contract).
	assert.Equal(t, sharedCloud, old)

	// New value is set.
	v, err := e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "gcp", v)

	// Shared data unchanged.
	assert.Equal(t, "aws", sharedCloud["provider"])
}

func TestCowMapDeleteLeaf(t *testing.T) {
	e := newCowEvent()

	err := e.Delete("cloud.region")
	require.NoError(t, err)

	_, err = e.GetValue("cloud.region")
	assert.Error(t, err)

	// Other cloud fields still present.
	v, err := e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "aws", v)

	// Shared data unchanged.
	assert.Equal(t, "us-east-1", sharedCloud["region"])
}

func TestCowMapDeleteEntireSubtree(t *testing.T) {
	e := newCowEvent()

	err := e.Delete("cloud")
	require.NoError(t, err)

	_, err = e.GetValue("cloud")
	assert.Error(t, err)

	_, err = e.GetValue("cloud.provider")
	assert.Error(t, err)

	// Non-cow fields unaffected.
	v, err := e.GetValue("message")
	require.NoError(t, err)
	assert.Equal(t, "test log message", v)
}

func TestCowMapHasKey(t *testing.T) {
	e := newCowEvent()

	ok, err := e.HasKey("cloud")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = e.HasKey("cloud.provider")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = e.HasKey("cloud.instance.id")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = e.HasKey("cloud.nonexistent")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCowMapDeepUpdateMerge(t *testing.T) {
	e := newCowEvent()

	// DeepUpdate with a map that overlaps the cowMap key.
	e.DeepUpdate(mapstr.M{
		"cloud": mapstr.M{"project": mapstr.M{"id": "my-project"}},
	})

	// New field added.
	v, err := e.GetValue("cloud.project.id")
	require.NoError(t, err)
	assert.Equal(t, "my-project", v)

	// Existing fields preserved.
	v, err = e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "aws", v)

	// Shared data unchanged.
	_, ok := sharedCloud["project"]
	assert.False(t, ok)
}

func TestCowMapDeepUpdateNoOverwrite(t *testing.T) {
	e := newCowEvent()

	e.DeepUpdateNoOverwrite(mapstr.M{
		"cloud": mapstr.M{
			"provider": "gcp",                              // should NOT overwrite
			"project":  mapstr.M{"id": "added-no-overwrite"}, // should add
		},
	})

	// Existing field NOT overwritten.
	v, err := e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "aws", v)

	// New field added.
	v, err = e.GetValue("cloud.project.id")
	require.NoError(t, err)
	assert.Equal(t, "added-no-overwrite", v)
}

func TestCowMapCloneSharesReference(t *testing.T) {
	e := newCowEvent()
	c := e.Clone()

	// Both see the same cloud data.
	v1, _ := e.GetValue("cloud.provider")
	v2, _ := c.GetValue("cloud.provider")
	assert.Equal(t, v1, v2)

	// Mutating clone's cloud doesn't affect original.
	_, _ = c.PutValue("cloud.provider", "gcp")
	v1, _ = e.GetValue("cloud.provider")
	v2, _ = c.GetValue("cloud.provider")
	assert.Equal(t, "aws", v1)
	assert.Equal(t, "gcp", v2)

	// Shared data unchanged.
	assert.Equal(t, "aws", sharedCloud["provider"])
}

func TestCowMapMaterialize(t *testing.T) {
	e := newCowEvent()
	m := e.Materialize()

	// Materialized map has cloud as plain mapstr.M.
	cloud, ok := m["cloud"].(mapstr.M)
	assert.True(t, ok)
	assert.Equal(t, "aws", cloud["provider"])
}

func TestCowMapMaterializeIsZeroCopy(t *testing.T) {
	e := newCowEvent()
	m := e.Materialize()

	// The materialized cloud IS the shared reference.
	assert.Equal(t, sharedCloud, m["cloud"])
}

func TestCowMapMultipleCowFields(t *testing.T) {
	sharedHost := mapstr.M{
		"name": "server1",
		"os":   mapstr.M{"family": "linux"},
	}
	e := &Event{Timestamp: time.Now()}
	e.SetFields(mapstr.M{"message": "test"})
	_ = e.PutValueQuiet("cloud", newCowMap(sharedCloud))
	_ = e.PutValueQuiet("host", newCowMap(sharedHost))

	v, err := e.GetValue("cloud.provider")
	require.NoError(t, err)
	assert.Equal(t, "aws", v)

	v, err = e.GetValue("host.name")
	require.NoError(t, err)
	assert.Equal(t, "server1", v)

	// Mutate host, cloud unaffected.
	_, _ = e.PutValue("host.name", "modified")
	v, _ = e.GetValue("host.name")
	assert.Equal(t, "modified", v)

	// Cloud data still readable.
	v, _ = e.GetValue("cloud.provider")
	assert.Equal(t, "aws", v)
}

func TestCowMapString(t *testing.T) {
	e := newCowEvent()
	s := e.String()
	assert.Contains(t, s, "aws")
	assert.Contains(t, s, "us-east-1")

	// Cloud still readable after String().
	v, _ := e.GetValue("cloud.provider")
	assert.Equal(t, "aws", v)
}

func TestCowMapProcessorPipelineSimulation(t *testing.T) {
	// Simulate: multiple events share the same processor data.
	sharedAgent := mapstr.M{"id": "agent-uuid", "version": "8.12.0"}

	events := make([]*Event, 100)
	for i := range events {
		e := &Event{Timestamp: time.Now()}
		e.SetFields(mapstr.M{"message": "event"})
		_ = e.PutValueQuiet("cloud", newCowMap(sharedCloud))
		_ = e.PutValueQuiet("agent", newCowMap(sharedAgent))
		events[i] = e
	}

	// Each event can read shared data.
	for _, e := range events {
		v, err := e.GetValue("cloud.provider")
		require.NoError(t, err)
		assert.Equal(t, "aws", v)
	}

	// Mutate one event's cloud — others unaffected.
	_, _ = events[50].PutValue("cloud.provider", "azure")

	v, _ := events[50].GetValue("cloud.provider")
	assert.Equal(t, "azure", v)

	v, _ = events[49].GetValue("cloud.provider")
	assert.Equal(t, "aws", v)

	v, _ = events[51].GetValue("cloud.provider")
	assert.Equal(t, "aws", v)

	// Shared data unchanged.
	assert.Equal(t, "aws", sharedCloud["provider"])
}
