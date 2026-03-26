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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

func TestIsScalarOnly(t *testing.T) {
	tests := map[string]struct {
		input    mapstr.M
		expected bool
	}{
		"empty map": {
			input:    mapstr.M{},
			expected: true,
		},
		"flat scalars": {
			input:    mapstr.M{"a": "b", "c": 1, "d": true, "e": 3.14},
			expected: true,
		},
		"nested maps with scalars": {
			input: mapstr.M{
				"agent": mapstr.M{
					"name":    "host",
					"version": "8.12.0",
				},
			},
			expected: true,
		},
		"contains slice": {
			input:    mapstr.M{"tags": []interface{}{"a", "b"}},
			expected: false,
		},
		"contains mapstr slice": {
			input:    mapstr.M{"items": []mapstr.M{{"a": "b"}}},
			expected: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isScalarOnly(tc.input))
		})
	}
}

func TestShallowCloneRecursive(t *testing.T) {
	original := mapstr.M{
		"agent": mapstr.M{
			"name":    "host",
			"version": "8.12.0",
		},
		"scalar": "value",
	}

	cloned := shallowCloneRecursive(original)

	// Values should be equal.
	assert.Equal(t, original, cloned)

	// Top-level maps should be different instances.
	cloned["new_key"] = "new_value"
	assert.NotContains(t, original, "new_key")

	// Nested maps should be different instances.
	clonedAgent := cloned["agent"].(mapstr.M)
	clonedAgent["new_field"] = "new_value"
	originalAgent := original["agent"].(mapstr.M)
	assert.NotContains(t, originalAgent, "new_field")
}

func TestAddFieldsFastPath(t *testing.T) {
	t.Run("single key, key not in event", func(t *testing.T) {
		proc := NewAddFields(mapstr.M{
			"agent": mapstr.M{
				"name":    "my-host",
				"version": "8.12.0",
			},
		}, true, true)

		event := &beat.Event{
			Timestamp: time.Now(),
			Fields:    mapstr.M{"message": "hello"},
		}

		result, err := proc.Run(event)
		require.NoError(t, err)
		require.NotNil(t, result)

		agent, err := result.Fields.GetValue("agent")
		require.NoError(t, err)
		agentMap := agent.(mapstr.M)
		assert.Equal(t, "my-host", agentMap["name"])
		assert.Equal(t, "8.12.0", agentMap["version"])
	})

	t.Run("single key, key exists as map (merge)", func(t *testing.T) {
		proc := NewAddFields(mapstr.M{
			"agent": mapstr.M{
				"version": "8.12.0",
			},
		}, true, true)

		event := &beat.Event{
			Timestamp: time.Now(),
			Fields: mapstr.M{
				"agent": mapstr.M{
					"name": "existing-host",
				},
			},
		}

		result, err := proc.Run(event)
		require.NoError(t, err)
		agentMap := result.Fields["agent"].(mapstr.M)
		// Should have merged: both name and version present.
		assert.Equal(t, "existing-host", agentMap["name"])
		assert.Equal(t, "8.12.0", agentMap["version"])
	})

	t.Run("single key, key exists as non-map (overwrite)", func(t *testing.T) {
		proc := NewAddFields(mapstr.M{
			"agent": mapstr.M{
				"name": "my-host",
			},
		}, true, true)

		event := &beat.Event{
			Fields: mapstr.M{
				"agent": "was-a-string",
			},
		}

		result, err := proc.Run(event)
		require.NoError(t, err)
		agentMap := result.Fields["agent"].(mapstr.M)
		assert.Equal(t, "my-host", agentMap["name"])
	})

	t.Run("shared safety: mutations don't corrupt processor", func(t *testing.T) {
		fields := mapstr.M{
			"agent": mapstr.M{
				"name":    "original",
				"version": "8.12.0",
			},
		}
		proc := NewAddFields(fields, true, true)

		// Run on first event and mutate the result.
		event1 := &beat.Event{Fields: mapstr.M{}}
		result1, _ := proc.Run(event1)
		result1.Fields["agent"].(mapstr.M)["name"] = "mutated"

		// Run on second event — should still get "original".
		event2 := &beat.Event{Fields: mapstr.M{}}
		result2, _ := proc.Run(event2)
		assert.Equal(t, "original", result2.Fields["agent"].(mapstr.M)["name"])
	})

	t.Run("nil event fields", func(t *testing.T) {
		proc := NewAddFields(mapstr.M{
			"agent": mapstr.M{"name": "host"},
		}, true, true)

		event := &beat.Event{Fields: nil}
		result, err := proc.Run(event)
		require.NoError(t, err)
		assert.Equal(t, "host", result.Fields["agent"].(mapstr.M)["name"])
	})
}

func TestAddFieldsScalarOnlyOptimization(t *testing.T) {
	t.Run("scalar only fields use shallow clone", func(t *testing.T) {
		fields := mapstr.M{
			"ecs": mapstr.M{"version": "8.0.0"},
			"host": mapstr.M{"name": "my-host"},
		}
		proc := NewAddFields(fields, true, true).(*addFields)

		assert.True(t, proc.scalarOnly)
	})

	t.Run("fields with slices are not scalar only", func(t *testing.T) {
		fields := mapstr.M{
			"tags": []interface{}{"a", "b"},
		}
		proc := NewAddFields(fields, true, true).(*addFields)

		assert.False(t, proc.scalarOnly)
	})

	t.Run("scalar only shared safety", func(t *testing.T) {
		// Multi-key fields (no singleKey fast path) with scalarOnly.
		fields := mapstr.M{
			"ecs":  mapstr.M{"version": "8.0.0"},
			"host": mapstr.M{"name": "my-host"},
		}
		proc := NewAddFields(fields, true, true)

		event1 := &beat.Event{Fields: mapstr.M{}}
		result1, _ := proc.Run(event1)
		result1.Fields["ecs"].(mapstr.M)["mutated"] = "yes"

		event2 := &beat.Event{Fields: mapstr.M{}}
		result2, _ := proc.Run(event2)
		_, exists := result2.Fields["ecs"].(mapstr.M)["mutated"]
		assert.False(t, exists, "mutation from event1 should not leak to event2")
	})
}

func TestAddFieldsNoOverwrite(t *testing.T) {
	t.Run("no overwrite skips fast path", func(t *testing.T) {
		proc := NewAddFields(mapstr.M{
			"agent": mapstr.M{"name": "default"},
		}, true, false)

		event := &beat.Event{
			Fields: mapstr.M{
				"agent": mapstr.M{"name": "custom"},
			},
		}

		result, err := proc.Run(event)
		require.NoError(t, err)
		// Should keep "custom" since overwrite is false.
		assert.Equal(t, "custom", result.Fields["agent"].(mapstr.M)["name"])
	})
}
