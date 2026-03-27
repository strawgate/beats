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

// flatFields provides flat key-value storage for event fields.
// Keys are stored in dotted notation (e.g. "cloud.instance.id").
// This avoids nested map allocations during the processor pipeline.
type flatFields struct {
	data map[string]interface{}
}

// newFlatFields creates a flatFields with the given capacity.
func newFlatFields(capacity int) *flatFields {
	return &flatFields{data: make(map[string]interface{}, capacity)}
}

// flattenFrom populates the flat storage from a nested mapstr.M.
func (f *flatFields) flattenFrom(m mapstr.M) {
	f.flattenInto(m, "")
}

func (f *flatFields) flattenInto(m mapstr.M, prefix string) {
	for k, v := range m {
		var fullKey string
		if prefix == "" {
			fullKey = k
		} else {
			fullKey = prefix + "." + k
		}
		switch inner := v.(type) {
		case mapstr.M:
			f.flattenInto(inner, fullKey)
		case map[string]interface{}:
			f.flattenInto(mapstr.M(inner), fullKey)
		default:
			f.data[fullKey] = v
		}
	}
}

// get returns the value at the dotted key path.
// For leaf keys, returns the value directly.
// For parent keys, materializes a nested mapstr.M subtree.
func (f *flatFields) get(key string) (interface{}, error) {
	// Direct lookup — leaf key.
	if v, ok := f.data[key]; ok {
		return v, nil
	}

	// Check for parent key — materialize subtree.
	prefix := key + "."
	sub := mapstr.M{}
	for k, v := range f.data {
		if strings.HasPrefix(k, prefix) {
			subKey := k[len(prefix):]
			_, _ = sub.Put(subKey, v)
		}
	}
	if len(sub) > 0 {
		return sub, nil
	}

	return nil, mapstr.ErrKeyNotFound
}

// put sets a value at the dotted key path. If the value is a nested
// map, it's flattened into individual entries.
func (f *flatFields) put(key string, value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case mapstr.M:
		old, _ := f.get(key)
		// Remove any existing entries under this prefix.
		f.deletePrefix(key)
		f.flattenInto(v, key)
		return old, nil
	case map[string]interface{}:
		old, _ := f.get(key)
		f.deletePrefix(key)
		f.flattenInto(mapstr.M(v), key)
		return old, nil
	default:
		old := f.data[key]
		// Remove any existing nested entries (replacing a map with a scalar).
		f.deletePrefix(key)
		f.data[key] = value
		return old, nil
	}
}

// delete removes the value at the dotted key path and any children.
func (f *flatFields) delete(key string) error {
	found := false

	if _, ok := f.data[key]; ok {
		delete(f.data, key)
		found = true
	}

	prefix := key + "."
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			delete(f.data, k)
			found = true
		}
	}

	if !found {
		return mapstr.ErrKeyNotFound
	}
	return nil
}

// hasKey returns true if the key exists as a leaf or has children.
func (f *flatFields) hasKey(key string) bool {
	if _, ok := f.data[key]; ok {
		return true
	}
	prefix := key + "."
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// deepUpdate merges a mapstr.M by flattening it and copying entries.
// No nested map allocation — just flat key copies.
func (f *flatFields) deepUpdate(m mapstr.M) {
	f.flattenInto(m, "")
}

// deepUpdateNoOverwrite merges a mapstr.M but skips keys that exist.
func (f *flatFields) deepUpdateNoOverwrite(m mapstr.M) {
	f.flattenNoOverwrite(m, "")
}

func (f *flatFields) flattenNoOverwrite(m mapstr.M, prefix string) {
	for k, v := range m {
		var fullKey string
		if prefix == "" {
			fullKey = k
		} else {
			fullKey = prefix + "." + k
		}
		switch inner := v.(type) {
		case mapstr.M:
			f.flattenNoOverwrite(inner, fullKey)
		case map[string]interface{}:
			f.flattenNoOverwrite(mapstr.M(inner), fullKey)
		default:
			if _, exists := f.data[fullKey]; !exists {
				f.data[fullKey] = v
			}
		}
	}
}

// clone creates an independent copy.
func (f *flatFields) clone() *flatFields {
	c := &flatFields{data: make(map[string]interface{}, len(f.data))}
	for k, v := range f.data {
		c.data[k] = v
	}
	return c
}

// toMapstr materializes the flat storage into a nested mapstr.M.
// Uses direct map navigation instead of mapstr.M.Put to avoid
// the mapFind string scanning overhead.
func (f *flatFields) toMapstr() mapstr.M {
	result := mapstr.M{}
	for k, v := range f.data {
		dot := strings.IndexByte(k, '.')
		if dot < 0 {
			// Top-level key — direct assignment.
			result[k] = v
			continue
		}
		// Navigate/create the nested map path.
		m := result
		for dot >= 0 {
			segment := k[:dot]
			k = k[dot+1:]
			if sub, ok := m[segment].(mapstr.M); ok {
				m = sub
			} else {
				next := mapstr.M{}
				m[segment] = next
				m = next
			}
			dot = strings.IndexByte(k, '.')
		}
		m[k] = v
	}
	return result
}

// deletePrefix removes all entries with the given prefix.
func (f *flatFields) deletePrefix(key string) {
	prefix := key + "."
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			delete(f.data, k)
		}
	}
}

// len returns the number of leaf entries.
func (f *flatFields) len() int {
	return len(f.data)
}
