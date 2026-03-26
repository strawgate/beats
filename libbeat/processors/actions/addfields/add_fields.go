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

	// hasSpecialKeys is true if fields contain @timestamp or @metadata keys
	// at the top level. When false, we can bypass event.deepUpdate's special
	// key handling and call event.Fields.DeepUpdate directly.
	hasSpecialKeys bool

	// metaFields contains only the @metadata value when fields has @metadata
	// but no @timestamp. This allows splitting the update into a fast-path
	// Fields.DeepUpdate + a targeted Meta update, avoiding the overhead of
	// event.deepUpdate's delete/defer pattern.
	metaFields mapstr.M

	// fieldsOnly contains the fields without @metadata/@timestamp keys.
	// Used together with metaFields to avoid the generic deepUpdate path.
	fieldsOnly mapstr.M

	// singleKey is set when the fields map has exactly one top-level key
	// wrapping an inner mapstr.M (e.g. {"elastic_agent": {"id": "...", ...}}).
	// This is the dominant shape created by MakeFieldsProcessor/generateAddFieldsProcessor.
	// When set, Run() clones only the inner map and builds a temporary wrapper,
	// saving one map allocation per event vs cloning the entire tree.
	singleKey      string
	singleKeyInner mapstr.M
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
	_, hasTimestamp := fields[beat.TimestampFieldKey]
	metaValue, hasMeta := fields[beat.MetadataFieldKey]

	af := &addFields{
		fields:         fields,
		shared:         shared,
		overwrite:      overwrite,
		hasSpecialKeys: hasTimestamp || hasMeta,
	}

	// Pre-split fields with @metadata but no @timestamp for the optimized path.
	if hasMeta && !hasTimestamp {
		if metaMap, ok := metaValue.(mapstr.M); ok {
			af.metaFields = metaMap
			if len(fields) > 1 {
				af.fieldsOnly = make(mapstr.M, len(fields)-1)
				for k, v := range fields {
					if k != beat.MetadataFieldKey {
						af.fieldsOnly[k] = v
					}
				}
			}
		}
	}

	// Detect single-key wrapper shape: {"target": mapstr.M{...}}.
	// This is the dominant pattern from MakeFieldsProcessor and elastic agent.
	// When shared=true, we only need to clone the inner map, not the outer wrapper.
	if shared && !af.hasSpecialKeys && len(fields) == 1 {
		for k, v := range fields {
			if inner, ok := v.(mapstr.M); ok {
				af.singleKey = k
				af.singleKeyInner = inner
			}
		}
	}

	return af
}

func (af *addFields) Run(event *beat.Event) (*beat.Event, error) {
	if event == nil || len(af.fields) == 0 {
		return event, nil
	}

	// Fast path: fields contain only regular keys (no @timestamp or @metadata).
	// This is the common case for elastic agent processors (agent info, data_stream, etc.).
	// We bypass event.deepUpdate's special key checking and call Fields.DeepUpdate directly.
	if !af.hasSpecialKeys {
		if event.Fields == nil {
			event.Fields = mapstr.M{}
		}
		if af.singleKeyInner != nil {
			// Single-key wrapper optimization: clone only the inner map and
			// build a temporary outer wrapper. This saves one map allocation
			// per event vs cloning the full 2-level map tree, because the
			// wrapper is a 1-entry map that Go can stack-allocate.
			inner := af.singleKeyInner
			if af.shared {
				inner = inner.Clone()
			}
			wrapper := mapstr.M{af.singleKey: inner}
			if af.overwrite {
				event.Fields.DeepUpdate(wrapper)
			} else {
				event.Fields.DeepUpdateNoOverwrite(wrapper)
			}
		} else {
			fields := af.fields
			if af.shared {
				fields = fields.Clone()
			}
			if af.overwrite {
				event.Fields.DeepUpdate(fields)
			} else {
				event.Fields.DeepUpdateNoOverwrite(fields)
			}
		}
		return event, nil
	}

	// Optimized @metadata path: when fields contain @metadata but no @timestamp,
	// we split the update to avoid event.deepUpdate's delete/defer overhead.
	if af.metaFields != nil {
		metaFields := af.metaFields
		if af.shared {
			metaFields = metaFields.Clone()
		}
		if event.Meta == nil {
			event.Meta = mapstr.M{}
		}
		if af.overwrite {
			event.Meta.DeepUpdate(metaFields)
		} else {
			event.Meta.DeepUpdateNoOverwrite(metaFields)
		}
		// Update remaining non-metadata fields if any
		if len(af.fieldsOnly) > 0 {
			fieldsOnly := af.fieldsOnly
			if af.shared {
				fieldsOnly = fieldsOnly.Clone()
			}
			if event.Fields == nil {
				event.Fields = mapstr.M{}
			}
			if af.overwrite {
				event.Fields.DeepUpdate(fieldsOnly)
			} else {
				event.Fields.DeepUpdateNoOverwrite(fieldsOnly)
			}
		}
		return event, nil
	}

	// Slow path: fields contain @timestamp or both @timestamp and @metadata.
	// Fall back to the generic event.deepUpdate which handles all special keys.
	fields := af.fields
	if af.shared {
		fields = fields.Clone()
	}
	if af.overwrite {
		event.DeepUpdate(fields)
	} else {
		event.DeepUpdateNoOverwrite(fields)
	}
	return event, nil
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
