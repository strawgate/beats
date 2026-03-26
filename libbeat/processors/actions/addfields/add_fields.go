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
	"encoding/json"
	"fmt"

	"github.com/elastic/beats/v7/libbeat/beat"
	conf "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

type addFields struct {
	fields    mapstr.M
	shared    bool
	overwrite bool

	// scalarOnly is true when fields (recursively) contains no nested
	// mapstr.M or map[string]interface{} values. When true, Clone() can
	// be skipped entirely because DeepUpdate only shares references to
	// map containers, and scalar values are immutable in Go.
	scalarOnly bool

	// singleKey is set when fields has exactly one top-level key and
	// that key's value is a mapstr.M. This enables a fast path that
	// avoids the full DeepUpdate machinery when the target key doesn't
	// exist in the event yet.
	singleKey      string
	singleKeyValue mapstr.M
}

// FieldsKey is the default target key for the add_fields processor.
const FieldsKey = "fields"

// CreateAddFields constructs an add_fields processor from config.
func CreateAddFields(c *conf.C, _ *logp.Logger) (beat.Processor, error) {
	config := struct {
		Fields mapstr.M `config:"fields" validate:"required"`
		Target *string  `config:"target"`
	}{}
	err := c.Unpack(&config)
	if err != nil {
		return nil, fmt.Errorf("fail to unpack the add_fields configuration: %w", err)
	}

	return MakeFieldsProcessor(
		optTarget(config.Target, FieldsKey),
		config.Fields,
		true,
	), nil
}

// NewAddFields creates a new processor adding the given fields to events.
// Set `shared` true if there is the chance of labels being changed/modified by
// subsequent processors.
func NewAddFields(fields mapstr.M, shared bool, overwrite bool) beat.Processor {
	af := &addFields{fields: fields, shared: shared, overwrite: overwrite}

	// Pre-compute optimization hints at construction time.
	af.scalarOnly = isScalarOnly(fields)

	// Detect single-key pattern (e.g., {"agent": {...}}) for fast-path.
	if len(fields) == 1 {
		for k, v := range fields {
			if m, ok := v.(mapstr.M); ok {
				af.singleKey = k
				af.singleKeyValue = m
			}
		}
	}

	return af
}

func (af *addFields) Run(event *beat.Event) (*beat.Event, error) {
	if event == nil || len(af.fields) == 0 {
		return event, nil
	}

	// Fast path: single top-level key with map value (e.g. {"agent": {...}}).
	// When the key doesn't exist in the event yet (common for enrichment),
	// we can skip DeepUpdate entirely and assign the value directly.
	// This avoids both the Clone() and DeepUpdate overhead.
	if af.singleKey != "" && af.overwrite {
		if event.Fields == nil {
			event.Fields = mapstr.M{}
		}
		existing, exists := event.Fields[af.singleKey]
		if !exists {
			// Key doesn't exist — assign directly, cloning only if shared.
			if af.shared {
				event.Fields[af.singleKey] = shallowCloneRecursive(af.singleKeyValue)
			} else {
				event.Fields[af.singleKey] = af.singleKeyValue
			}
			return event, nil
		}
		// Key exists but is not a map — overwrite directly.
		if _, isMap := existing.(mapstr.M); !isMap {
			if _, isRawMap := existing.(map[string]interface{}); !isRawMap {
				if af.shared {
					event.Fields[af.singleKey] = shallowCloneRecursive(af.singleKeyValue)
				} else {
					event.Fields[af.singleKey] = af.singleKeyValue
				}
				return event, nil
			}
		}
		// Key exists as a map — fall through to DeepUpdate for recursive merge.
	}

	fields := af.fields
	if af.shared {
		if af.scalarOnly {
			// All leaf values are scalars (strings, ints, etc.) which are
			// immutable in Go. DeepUpdate only shares references to map
			// containers, so we only need to clone the map skeleton.
			fields = shallowCloneRecursive(fields)
		} else {
			fields = fields.Clone()
		}
	}

	if af.overwrite {
		event.DeepUpdate(fields)
	} else {
		event.DeepUpdateNoOverwrite(fields)
	}

	return event, nil
}

// isScalarOnly returns true if m (recursively) contains only scalar values
// (no slices of maps or other complex types that need deep cloning).
func isScalarOnly(m mapstr.M) bool {
	for _, v := range m {
		switch val := v.(type) {
		case mapstr.M:
			if !isScalarOnly(val) {
				return false
			}
		case map[string]interface{}:
			if !isScalarOnly(mapstr.M(val)) {
				return false
			}
		case []interface{}:
			// Slices may contain maps that need deep cloning.
			return false
		case []mapstr.M:
			return false
		case []map[string]interface{}:
			return false
		}
	}
	return true
}

// shallowCloneRecursive creates new map containers at every level but
// shares scalar leaf values. This is cheaper than Clone() which also
// copies slices of maps and does full deep copies.
func shallowCloneRecursive(m mapstr.M) mapstr.M {
	result := make(mapstr.M, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case mapstr.M:
			result[k] = shallowCloneRecursive(val)
		case map[string]interface{}:
			result[k] = shallowCloneRecursive(mapstr.M(val))
		default:
			result[k] = v
		}
	}
	return result
}

func (af *addFields) String() string {
	s, _ := json.Marshal(af.fields)
	return fmt.Sprintf("add_fields=%s", s)
}

func optTarget(opt *string, def string) string {
	if opt == nil {
		return def
	}
	return *opt
}

func MakeFieldsProcessor(target string, fields mapstr.M, shared bool) beat.Processor {
	if target != "" {
		fields = mapstr.M{
			target: fields,
		}
	}

	return NewAddFields(fields, shared, true)
}
