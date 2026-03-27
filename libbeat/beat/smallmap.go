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

const smallMapCap = 20

type fieldEntry struct {
	key   string
	value interface{}
}

// SmallMap is a flat key-value store optimized for small numbers of entries.
// Below smallMapCap entries it uses a fixed-size array (zero allocation,
// cache-friendly). Above that it promotes to a Go map.
// SmallMap is pure storage — no dot parsing, no cowMap awareness.
type SmallMap struct {
	entries [smallMapCap]fieldEntry
	n       int
	m       mapstr.M // nil until promoted
}

// Get returns the value for key, or (nil, false) if not found.
func (s *SmallMap) Get(key string) (interface{}, bool) {
	if s.m != nil {
		v, ok := s.m[key]
		return v, ok
	}
	for i := 0; i < s.n; i++ {
		if s.entries[i].key == key {
			return s.entries[i].value, true
		}
	}
	return nil, false
}

// Set stores a key-value pair. Overwrites if key exists.
func (s *SmallMap) Set(key string, value interface{}) {
	if s.m != nil {
		s.m[key] = value
		return
	}
	for i := 0; i < s.n; i++ {
		if s.entries[i].key == key {
			s.entries[i].value = value
			return
		}
	}
	if s.n < smallMapCap {
		s.entries[s.n] = fieldEntry{key, value}
		s.n++
		return
	}
	// Promote to map.
	s.m = make(mapstr.M, smallMapCap*2)
	for i := 0; i < s.n; i++ {
		s.m[s.entries[i].key] = s.entries[i].value
		s.entries[i] = fieldEntry{}
	}
	s.n = 0
	s.m[key] = value
}

// Delete removes a key. Returns true if the key existed.
func (s *SmallMap) Delete(key string) bool {
	if s.m != nil {
		if _, ok := s.m[key]; ok {
			delete(s.m, key)
			return true
		}
		return false
	}
	for i := 0; i < s.n; i++ {
		if s.entries[i].key == key {
			copy(s.entries[i:], s.entries[i+1:s.n])
			s.n--
			s.entries[s.n] = fieldEntry{}
			return true
		}
	}
	return false
}

// Has returns true if the key exists.
func (s *SmallMap) Has(key string) bool {
	if s.m != nil {
		_, ok := s.m[key]
		return ok
	}
	for i := 0; i < s.n; i++ {
		if s.entries[i].key == key {
			return true
		}
	}
	return false
}

// Len returns the number of entries.
func (s *SmallMap) Len() int {
	if s.m != nil {
		return len(s.m)
	}
	return s.n
}

// Range calls fn for each key-value pair. If fn returns false, iteration stops.
func (s *SmallMap) Range(fn func(key string, value interface{}) bool) {
	if s.m != nil {
		for k, v := range s.m {
			if !fn(k, v) {
				return
			}
		}
		return
	}
	for i := 0; i < s.n; i++ {
		if !fn(s.entries[i].key, s.entries[i].value) {
			return
		}
	}
}

// ToMapStr renders the SmallMap into a mapstr.M. Values are copied
// by reference (no deep cloning).
func (s *SmallMap) ToMapStr() mapstr.M {
	if s.m != nil {
		return s.m
	}
	if s.n == 0 {
		return nil
	}
	result := make(mapstr.M, s.n)
	for i := 0; i < s.n; i++ {
		result[s.entries[i].key] = s.entries[i].value
	}
	return result
}

// Clone returns a shallow copy. Values are NOT deep-cloned —
// the caller is responsible for cloning map/cowMap values as needed.
func (s *SmallMap) Clone() SmallMap {
	var c SmallMap
	if s.m != nil {
		c.m = make(mapstr.M, len(s.m))
		for k, v := range s.m {
			c.m[k] = v
		}
		return c
	}
	c.n = s.n
	copy(c.entries[:s.n], s.entries[:s.n])
	return c
}

// Clear resets to empty state. The inline array is zeroed and
// the overflow map is dropped.
func (s *SmallMap) Clear() {
	for i := 0; i < s.n; i++ {
		s.entries[i] = fieldEntry{}
	}
	s.n = 0
	s.m = nil
}

// IsPromoted returns true if the SmallMap has been promoted to a Go map.
func (s *SmallMap) IsPromoted() bool {
	return s.m != nil
}

// SmallMapFromMapStr creates a SmallMap populated from a mapstr.M.
func SmallMapFromMapStr(m mapstr.M) SmallMap {
	var s SmallMap
	for k, v := range m {
		s.Set(k, v)
	}
	return s
}

