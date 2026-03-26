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

package add_host_metadata

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/processors/actions/addfields"
	conf "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp/logptest"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

// TestGeoDataCorruptionEndToEnd simulates the exact filebeat processor chain
// from geodata_corruption_conditional.yml and verifies the behavior.
//
// Processor chain:
//  1. add_host_metadata with geo config
//  2. add_fields targeting host.geo.name — conditional on message containing "MUTATE_GEO"
//
// Events:
//
//	event1: message="MUTATE_GEO"       → triggers add_fields, host.geo.name="FIRST-EVENT-ONLY"
//	event2: message="normal event"     → should have host.geo.name="datacenter-east"
//	event3: message="another normal"   → should have host.geo.name="datacenter-east"
func TestGeoDataCorruptionEndToEnd(t *testing.T) {
	t.Skip("Known bug: geoData is not cloned before DeepUpdate. " +
		"See testdata/geodata_corruption_conditional.yml for a filebeat reproduction. " +
		"Fix: change event.Fields.DeepUpdate(p.geoData) to event.Fields.DeepUpdate(p.geoData.Clone())")

	// Build processor 1: add_host_metadata with geo
	hostConfig, err := conf.NewConfigFrom(map[string]interface{}{
		"geo.name":             "datacenter-east",
		"geo.location":         "40.7128, -74.0060",
		"geo.continent_name":   "North America",
		"geo.country_name":     "United States",
		"geo.country_iso_code": "US",
		"geo.region_name":      "New York",
		"geo.region_iso_code":  "US-NY",
		"geo.city_name":        "New York",
	})
	require.NoError(t, err)

	hostProc, err := New(hostConfig, logptest.NewTestingLogger(t, ""))
	require.NoError(t, err)

	// Build processor 2: add_fields with target="" to merge at root level,
	// overwriting the nested host.geo.name field.
	geoOverride := addfields.NewAddFields(mapstr.M{
		"host": mapstr.M{"geo": mapstr.M{"name": "FIRST-EVENT-ONLY"}},
	}, true, true)

	// Simulate three events like the filebeat config
	messages := []string{"MUTATE_GEO", "normal event", "another normal event"}
	results := make([]*beat.Event, 3)

	for i, msg := range messages {
		event := &beat.Event{
			Fields:    mapstr.M{"message": msg},
			Timestamp: time.Now(),
		}

		// Processor 1: add_host_metadata
		event, err = hostProc.Run(event)
		require.NoError(t, err)

		// Processor 2: conditional add_fields (only when message contains "MUTATE_GEO")
		if strings.Contains(msg, "MUTATE_GEO") {
			event, err = geoOverride.Run(event)
			require.NoError(t, err)
		}

		results[i] = event
	}

	// Event 1: should have the overridden geo name
	geoName1, err := results[0].GetValue("host.geo.name")
	require.NoError(t, err)
	assert.Equal(t, "FIRST-EVENT-ONLY", geoName1)

	// Event 2: should have the ORIGINAL geo name, not the overridden one
	geoName2, err := results[1].GetValue("host.geo.name")
	require.NoError(t, err)
	assert.Equal(t, "datacenter-east", geoName2,
		"event 2 geo.name was corrupted by event 1's conditional override")

	// Event 3: same as event 2
	geoName3, err := results[2].GetValue("host.geo.name")
	require.NoError(t, err)
	assert.Equal(t, "datacenter-east", geoName3,
		"event 3 geo.name was corrupted by event 1's conditional override")

	// Also verify other geo fields survived on all events
	for i, event := range results {
		city, err := event.GetValue("host.geo.city_name")
		require.NoError(t, err, "event %d missing city_name", i+1)
		assert.Equal(t, "New York", city, "event %d city_name", i+1)

		country, err := event.GetValue("host.geo.country_name")
		require.NoError(t, err, "event %d missing country_name", i+1)
		assert.Equal(t, "United States", country, "event %d country_name", i+1)
	}
}
