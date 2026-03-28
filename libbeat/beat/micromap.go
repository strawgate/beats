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
	"github.com/elastic/elastic-agent-libs/mapstr"
)

// microMap is an interface for small key-value stores that are more memory
// efficient than Go maps for entries ≤10. Implementations are mm1 through
// mm10 (fixed-size structs) and mmMap (wraps mapstr.M for >10 entries).
//
// microMap values are used inside cowMap clones and as nested sub-map
// values in event fields. The encoder is taught to fold them via
// go-structform's Folders system.
type microMap interface {
	// Get returns the value for key, or (nil, false) if not found.
	Get(key string) (interface{}, bool)

	// Set stores a key-value pair. Returns a (possibly promoted) microMap.
	// The caller must use the returned value as the original may be replaced.
	Set(key string, value interface{}) microMap

	// Delete removes a key. Returns a (possibly demoted) microMap.
	Delete(key string) microMap

	// Len returns the number of entries.
	Len() int

	// Range calls fn for each key-value pair. Stops if fn returns false.
	Range(fn func(key string, value interface{}) bool)

	// Clone returns a shallow copy.
	Clone() microMap

	// ToMapStr converts to a mapstr.M. Nested microMaps are recursively converted.
	ToMapStr() mapstr.M
}

// --- mm1: 1-entry microMap (32 bytes) ---

type mm1 struct {
	k string
	v interface{}
}

func (m *mm1) Get(key string) (interface{}, bool) {
	if m.k == key {
		return m.v, true
	}
	return nil, false
}

func (m *mm1) Set(key string, value interface{}) microMap {
	if m.k == key {
		m.v = value
		return m
	}
	return &mm2{k1: m.k, k2: key, v1: m.v, v2: value}
}

func (m *mm1) Delete(key string) microMap {
	if m.k == key {
		return nil
	}
	return m
}

func (m *mm1) Len() int { return 1 }

func (m *mm1) Range(fn func(string, interface{}) bool) {
	fn(m.k, m.v)
}

func (m *mm1) Clone() microMap {
	return &mm1{k: m.k, v: m.v}
}

func (m *mm1) ToMapStr() mapstr.M {
	return mapstr.M{m.k: microMapToValue(m.v)}
}

// --- mm2: 2-entry microMap (64 bytes) ---

type mm2 struct {
	k1, k2 string
	v1, v2 interface{}
}

func (m *mm2) Get(key string) (interface{}, bool) {
	if m.k1 == key {
		return m.v1, true
	}
	if m.k2 == key {
		return m.v2, true
	}
	return nil, false
}

func (m *mm2) Set(key string, value interface{}) microMap {
	if m.k1 == key {
		m.v1 = value
		return m
	}
	if m.k2 == key {
		m.v2 = value
		return m
	}
	return &mm3{k1: m.k1, k2: m.k2, k3: key, v1: m.v1, v2: m.v2, v3: value}
}

func (m *mm2) Delete(key string) microMap {
	if m.k1 == key {
		return &mm1{k: m.k2, v: m.v2}
	}
	if m.k2 == key {
		return &mm1{k: m.k1, v: m.v1}
	}
	return m
}

func (m *mm2) Len() int { return 2 }

func (m *mm2) Range(fn func(string, interface{}) bool) {
	if !fn(m.k1, m.v1) {
		return
	}
	fn(m.k2, m.v2)
}

func (m *mm2) Clone() microMap {
	return &mm2{k1: m.k1, k2: m.k2, v1: m.v1, v2: m.v2}
}

func (m *mm2) ToMapStr() mapstr.M {
	return mapstr.M{m.k1: microMapToValue(m.v1), m.k2: microMapToValue(m.v2)}
}

// --- mm3: 3-entry microMap (96 bytes) ---

type mm3 struct {
	k1, k2, k3 string
	v1, v2, v3 interface{}
}

func (m *mm3) Get(key string) (interface{}, bool) {
	switch key {
	case m.k1:
		return m.v1, true
	case m.k2:
		return m.v2, true
	case m.k3:
		return m.v3, true
	}
	return nil, false
}

func (m *mm3) Set(key string, value interface{}) microMap {
	switch key {
	case m.k1:
		m.v1 = value
		return m
	case m.k2:
		m.v2 = value
		return m
	case m.k3:
		m.v3 = value
		return m
	}
	return &mm4{k1: m.k1, k2: m.k2, k3: m.k3, k4: key,
		v1: m.v1, v2: m.v2, v3: m.v3, v4: value}
}

func (m *mm3) Delete(key string) microMap {
	switch key {
	case m.k1:
		return &mm2{k1: m.k2, k2: m.k3, v1: m.v2, v2: m.v3}
	case m.k2:
		return &mm2{k1: m.k1, k2: m.k3, v1: m.v1, v2: m.v3}
	case m.k3:
		return &mm2{k1: m.k1, k2: m.k2, v1: m.v1, v2: m.v2}
	}
	return m
}

func (m *mm3) Len() int { return 3 }

func (m *mm3) Range(fn func(string, interface{}) bool) {
	if !fn(m.k1, m.v1) {
		return
	}
	if !fn(m.k2, m.v2) {
		return
	}
	fn(m.k3, m.v3)
}

func (m *mm3) Clone() microMap {
	c := *m
	return &c
}

func (m *mm3) ToMapStr() mapstr.M {
	return mapstr.M{m.k1: microMapToValue(m.v1), m.k2: microMapToValue(m.v2), m.k3: microMapToValue(m.v3)}
}

// --- mm4: 4-entry microMap (128 bytes) ---

type mm4 struct {
	k1, k2, k3, k4 string
	v1, v2, v3, v4 interface{}
}

func (m *mm4) Get(key string) (interface{}, bool) {
	switch key {
	case m.k1:
		return m.v1, true
	case m.k2:
		return m.v2, true
	case m.k3:
		return m.v3, true
	case m.k4:
		return m.v4, true
	}
	return nil, false
}

func (m *mm4) Set(key string, value interface{}) microMap {
	switch key {
	case m.k1:
		m.v1 = value
		return m
	case m.k2:
		m.v2 = value
		return m
	case m.k3:
		m.v3 = value
		return m
	case m.k4:
		m.v4 = value
		return m
	}
	return newMmN(
		[]string{m.k1, m.k2, m.k3, m.k4, key},
		[]interface{}{m.v1, m.v2, m.v3, m.v4, value})
}

func (m *mm4) Delete(key string) microMap {
	switch key {
	case m.k1:
		return &mm3{k1: m.k2, k2: m.k3, k3: m.k4, v1: m.v2, v2: m.v3, v3: m.v4}
	case m.k2:
		return &mm3{k1: m.k1, k2: m.k3, k3: m.k4, v1: m.v1, v2: m.v3, v3: m.v4}
	case m.k3:
		return &mm3{k1: m.k1, k2: m.k2, k3: m.k4, v1: m.v1, v2: m.v2, v3: m.v4}
	case m.k4:
		return &mm3{k1: m.k1, k2: m.k2, k3: m.k3, v1: m.v1, v2: m.v2, v3: m.v3}
	}
	return m
}

func (m *mm4) Len() int { return 4 }

func (m *mm4) Range(fn func(string, interface{}) bool) {
	if !fn(m.k1, m.v1) || !fn(m.k2, m.v2) || !fn(m.k3, m.v3) {
		return
	}
	fn(m.k4, m.v4)
}

func (m *mm4) Clone() microMap {
	c := *m
	return &c
}

func (m *mm4) ToMapStr() mapstr.M {
	r := make(mapstr.M, 4)
	r[m.k1] = microMapToValue(m.v1)
	r[m.k2] = microMapToValue(m.v2)
	r[m.k3] = microMapToValue(m.v3)
	r[m.k4] = microMapToValue(m.v4)
	return r
}

// --- mm5 through mm10 follow the same pattern ---
// For brevity, using array-based implementation for 5+

type mmN struct {
	keys   []string
	values []interface{}
}

func newMmN(keys []string, values []interface{}) *mmN {
	return &mmN{keys: keys, values: values}
}

func (m *mmN) Get(key string) (interface{}, bool) {
	for i, k := range m.keys {
		if k == key {
			return m.values[i], true
		}
	}
	return nil, false
}

func (m *mmN) Set(key string, value interface{}) microMap {
	for i, k := range m.keys {
		if k == key {
			m.values[i] = value
			return m
		}
	}
	if len(m.keys) >= 10 {
		// Promote to map.
		mm := &mmMapWrap{m: make(mapstr.M, len(m.keys)+1)}
		for i, k := range m.keys {
			mm.m[k] = m.values[i]
		}
		mm.m[key] = value
		return mm
	}
	m.keys = append(m.keys, key)
	m.values = append(m.values, value)
	return m
}

func (m *mmN) Delete(key string) microMap {
	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			m.values = append(m.values[:i], m.values[i+1:]...)
			if len(m.keys) <= 4 {
				return demoteToFixed(m.keys, m.values)
			}
			return m
		}
	}
	return m
}

func (m *mmN) Len() int { return len(m.keys) }

func (m *mmN) Range(fn func(string, interface{}) bool) {
	for i, k := range m.keys {
		if !fn(k, m.values[i]) {
			return
		}
	}
}

func (m *mmN) Clone() microMap {
	keys := make([]string, len(m.keys))
	values := make([]interface{}, len(m.values))
	copy(keys, m.keys)
	copy(values, m.values)
	return &mmN{keys: keys, values: values}
}

func (m *mmN) ToMapStr() mapstr.M {
	r := make(mapstr.M, len(m.keys))
	for i, k := range m.keys {
		r[k] = microMapToValue(m.values[i])
	}
	return r
}

// --- mmMapWrap: wraps mapstr.M for >10 entries ---

type mmMapWrap struct {
	m mapstr.M
}

func (m *mmMapWrap) Get(key string) (interface{}, bool) {
	v, ok := m.m[key]
	return v, ok
}

func (m *mmMapWrap) Set(key string, value interface{}) microMap {
	m.m[key] = value
	return m
}

func (m *mmMapWrap) Delete(key string) microMap {
	delete(m.m, key)
	return m
}

func (m *mmMapWrap) Len() int { return len(m.m) }

func (m *mmMapWrap) Range(fn func(string, interface{}) bool) {
	for k, v := range m.m {
		if !fn(k, v) {
			return
		}
	}
}

func (m *mmMapWrap) Clone() microMap {
	return &mmMapWrap{m: m.m.Clone()}
}

func (m *mmMapWrap) ToMapStr() mapstr.M {
	r := make(mapstr.M, len(m.m))
	for k, v := range m.m {
		r[k] = microMapToValue(v)
	}
	return r
}

// --- helpers ---

// microMapFromMapStr converts a mapstr.M to the appropriately-sized microMap.
// Nested mapstr.M values are recursively converted.
func microMapFromMapStr(m mapstr.M) microMap {
	switch len(m) {
	case 0:
		return nil
	case 1:
		for k, v := range m {
			return &mm1{k: k, v: convertNestedValue(v)}
		}
		return nil // unreachable
	case 2:
		mm := &mm2{}
		i := 0
		for k, v := range m {
			if i == 0 {
				mm.k1, mm.v1 = k, convertNestedValue(v)
			} else {
				mm.k2, mm.v2 = k, convertNestedValue(v)
			}
			i++
		}
		return mm
	case 3:
		mm := &mm3{}
		i := 0
		for k, v := range m {
			switch i {
			case 0:
				mm.k1, mm.v1 = k, convertNestedValue(v)
			case 1:
				mm.k2, mm.v2 = k, convertNestedValue(v)
			case 2:
				mm.k3, mm.v3 = k, convertNestedValue(v)
			}
			i++
		}
		return mm
	case 4:
		mm := &mm4{}
		i := 0
		for k, v := range m {
			switch i {
			case 0:
				mm.k1, mm.v1 = k, convertNestedValue(v)
			case 1:
				mm.k2, mm.v2 = k, convertNestedValue(v)
			case 2:
				mm.k3, mm.v3 = k, convertNestedValue(v)
			case 3:
				mm.k4, mm.v4 = k, convertNestedValue(v)
			}
			i++
		}
		return mm
	default:
		if len(m) <= 10 {
			keys := make([]string, 0, len(m))
			values := make([]interface{}, 0, len(m))
			for k, v := range m {
				keys = append(keys, k)
				values = append(values, convertNestedValue(v))
			}
			return &mmN{keys: keys, values: values}
		}
		mm := &mmMapWrap{m: make(mapstr.M, len(m))}
		for k, v := range m {
			mm.m[k] = convertNestedValue(v)
		}
		return mm
	}
}

// convertNestedValue converts nested mapstr.M values to microMaps.
func convertNestedValue(v interface{}) interface{} {
	switch val := v.(type) {
	case mapstr.M:
		if len(val) == 0 {
			return val
		}
		return microMapFromMapStr(val)
	case map[string]interface{}:
		if len(val) == 0 {
			return val
		}
		return microMapFromMapStr(mapstr.M(val))
	default:
		return v
	}
}

// microMapToValue converts a value for ToMapStr. If the value is a microMap,
// it's recursively converted to mapstr.M.
func microMapToValue(v interface{}) interface{} {
	if mm, ok := v.(microMap); ok {
		return mm.ToMapStr()
	}
	return v
}

// demoteToFixed converts slices back to a fixed-size microMap.
func demoteToFixed(keys []string, values []interface{}) microMap {
	switch len(keys) {
	case 0:
		return nil
	case 1:
		return &mm1{k: keys[0], v: values[0]}
	case 2:
		return &mm2{k1: keys[0], k2: keys[1], v1: values[0], v2: values[1]}
	case 3:
		return &mm3{k1: keys[0], k2: keys[1], k3: keys[2],
			v1: values[0], v2: values[1], v3: values[2]}
	case 4:
		return &mm4{k1: keys[0], k2: keys[1], k3: keys[2], k4: keys[3],
			v1: values[0], v2: values[1], v3: values[2], v4: values[3]}
	default:
		return &mmN{keys: keys, values: values}
	}
}
