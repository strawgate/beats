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
	"strings"

	"github.com/elastic/elastic-agent-libs/mapstr"
)

// cowMap is a copy-on-write wrapper around a shared mapstr.M.
// Multiple events can share the same underlying data without cloning.
// The shared data is only copied when a write targets this sub-tree.
//
// cowMap values are stored in event.Fields at top-level keys
// (e.g., Fields["cloud"] = newCowMap(sharedCloudData)).
// Event accessor methods handle cowMap transparently.
type cowMap struct {
	shared mapstr.M
}

// newCowMap creates a cowMap wrapping shared data.
// The shared map must not be modified after wrapping.
func newCowMap(shared mapstr.M) *cowMap {
	return &cowMap{shared: shared}
}

// NewCowMap creates a copy-on-write wrapper for shared data.
// Use this in processors to store shared metadata in event.Fields
// without per-event cloning. The shared map must not be modified
// after wrapping.
func NewCowMap(shared mapstr.M) *cowMap {
	return &cowMap{shared: shared}
}

// cowField checks if the top-level key segment of key holds a cowMap
// in the event's Fields. Returns the top-level key, the cowMap, and
// the remaining sub-key (empty if key is a top-level key).
func (e *Event) cowField(key string) (topKey, subKey string, cm *cowMap) {
	if !e.hasCow {
		return "", "", nil
	}
	dot := strings.IndexByte(key, '.')
	if dot < 0 {
		topKey = key
	} else {
		topKey = key[:dot]
	}
	v, ok := e.inlineGet(topKey)
	if !ok {
		return "", "", nil
	}
	cow, ok := v.(*cowMap)
	if !ok {
		return "", "", nil
	}
	if dot < 0 {
		return topKey, "", cow
	}
	return topKey, key[dot+1:], cow
}

// materializeCow replaces the cowMap at topKey with a mutable clone
// of the shared data and returns the clone.
func (e *Event) materializeCow(topKey string, cm *cowMap) mapstr.M {
	cloned := cm.shared.Clone()
	e.inlineSet(topKey, cloned)
	return cloned
}

// materializeCowsForUpdate replaces any cowMaps in Fields that would
// be merged into by DeepUpdate with the given update map.
func (e *Event) materializeCowsForUpdate(d mapstr.M) {
	if !e.hasCow {
		return
	}
	for k, v := range d {
		val, ok := e.inlineGet(k)
		if !ok {
			continue
		}
		cow, ok := val.(*cowMap)
		if !ok {
			continue
		}
		switch v.(type) {
		case mapstr.M, map[string]interface{}:
			e.inlineSet(k, cow.shared.Clone())
		}
	}
}

// Materialize replaces all cowMap values in Fields with their underlying
// shared mapstr.M references. This is zero-allocation and safe for
// read-only operations like encoding. Call before passing the event
// to the output encoder.
// Materialize builds the Fields map from inline entries, unwrapping
// any cowMap values to their underlying shared mapstr.M references.
// Call before passing the event to the encoder or any code that
// reads Fields directly.
func (e *Event) Materialize() {
	if e.nFields == 0 && e.overflow == nil {
		return
	}
	if e.fields == nil {
		e.fields = make(mapstr.M, e.nFields+len(e.overflow))
	} else {
		clear(e.fields)
	}
	for i := 0; i < e.nFields; i++ {
		v := e.entries[i].value
		if cm, ok := v.(*cowMap); ok {
			v = cm.shared
		}
		e.fields[e.entries[i].key] = v
	}
	for k, v := range e.overflow {
		if cm, ok := v.(*cowMap); ok {
			v = cm.shared
		}
		e.fields[k] = v
	}
	e.hasCow = false
}
