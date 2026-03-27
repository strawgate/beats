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
	"github.com/elastic/beats/v7/libbeat/common/mapstrutil"
	conf "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

type addFields struct {
	fields    mapstr.M
	shared    bool
	overwrite bool

	// metaFields contains only the @metadata value when fields has @metadata
	// but no @timestamp. This allows splitting the update into a fast-path
	// Fields.DeepUpdate + a targeted Meta update, avoiding the overhead of
	// event.deepUpdate's delete/defer pattern.
	metaFields mapstr.M

	// fieldsOnly contains the fields without @metadata/@timestamp keys.
	// Used together with metaFields to avoid the generic deepUpdate path.
	fieldsOnly mapstr.M

	// cowFields is pre-allocated cowMap wrappers for shared fields.
	// Each map-valued top-level key gets a cowMap, reused across all events
	// (zero per-event allocation). Covers both single-key
	// (e.g. {"elastic_agent": {...}}) and multi-key (e.g. {ecs, host, agent}).
	cowFields map[string]interface{}
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
		fields:    fields,
		shared:    shared,
		overwrite: overwrite,
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

	// Pre-allocate cowMap wrappers for shared fields without special keys.
	// Covers both single-key (e.g. {"elastic_agent": {...}}) and multi-key
	// (e.g. builtin {ecs, host, agent}) processors.
	if shared && !hasTimestamp && !hasMeta && len(fields) > 0 {
		allMaps := true
		for _, v := range fields {
			if _, ok := v.(mapstr.M); !ok {
				allMaps = false
				break
			}
		}
		if allMaps {
			af.cowFields = make(map[string]interface{}, len(fields))
			for k, v := range fields {
				af.cowFields[k] = beat.NewCowMap(v.(mapstr.M))
			}
		}
	}

	return af
}

func (af *addFields) Run(event *beat.Event) (*beat.Event, error) {
	if event == nil || len(af.fields) == 0 {
		return event, nil
	}

	// Metadata split path: when fields contain @metadata but no @timestamp,
	// update event.Meta and event.Fields separately. This avoids cloning the
	// outer {"@metadata": inner} wrapper and bypasses event.deepUpdate's
	// delete/defer pattern for @metadata handling.
	if af.metaFields != nil {
		if event.Meta == nil {
			event.Meta = mapstr.M{}
		}
		if af.shared && af.overwrite {
			mapstrutil.DeepCopyUpdate(event.Meta, af.metaFields)
		} else if af.shared {
			mapstrutil.DeepCopyUpdateNoOverwrite(event.Meta, af.metaFields)
		} else if af.overwrite {
			event.Meta.DeepUpdate(af.metaFields)
		} else {
			event.Meta.DeepUpdateNoOverwrite(af.metaFields)
		}
		if len(af.fieldsOnly) > 0 {
			if af.overwrite {
				event.DeepUpdate(af.fieldsOnly)
			} else {
				event.DeepUpdateNoOverwrite(af.fieldsOnly)
			}
		}
		return event, nil
	}

	// Multi-key cowMap path: for shared fields with multiple top-level keys
	// (e.g. builtin {ecs, host, agent}), store cowMap for keys that don't
	// exist, merge for keys that do.
	if af.cowFields != nil {
		for k, cowVal := range af.cowFields {
			if exists, _ := event.HasKey(k); !exists {
				_ = event.PutValueQuiet(k, cowVal)
			} else if af.overwrite {
				event.DeepUpdate(mapstr.M{k: af.fields[k]})
			} else {
				event.DeepUpdateNoOverwrite(mapstr.M{k: af.fields[k]})
			}
		}
		return event, nil
	}

	// General path: handles @timestamp, @metadata, and regular fields.
	_, hasTimestamp := af.fields[beat.TimestampFieldKey]
	_, hasMeta := af.fields[beat.MetadataFieldKey]

	if !hasTimestamp && !hasMeta {
		if af.overwrite {
			event.DeepUpdate(af.fields)
		} else {
			event.DeepUpdateNoOverwrite(af.fields)
		}
		return event, nil
	}

	// Slow path: has @timestamp or @metadata, needs event.DeepUpdate
	// which handles those special keys.
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
