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
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/mapstr"
)

var keys10 = []string{"message", "cloud", "host", "agent", "elastic_agent", "data_stream", "event", "ecs", "error", "log"}

func TestSmallMapGetSet(t *testing.T) {
	var s SmallMap
	s.Set("foo", "bar")
	v, ok := s.Get("foo")
	require.True(t, ok)
	assert.Equal(t, "bar", v)
}

func TestSmallMapOverwrite(t *testing.T) {
	var s SmallMap
	s.Set("key", "v1")
	s.Set("key", "v2")
	v, ok := s.Get("key")
	require.True(t, ok)
	assert.Equal(t, "v2", v)
	assert.Equal(t, 1, s.Len())
}

func TestSmallMapDelete(t *testing.T) {
	var s SmallMap
	s.Set("a", 1)
	s.Set("b", 2)
	ok := s.Delete("a")
	assert.True(t, ok)
	assert.Equal(t, 1, s.Len())
	_, found := s.Get("a")
	assert.False(t, found)
	v, found := s.Get("b")
	assert.True(t, found)
	assert.Equal(t, 2, v)
}

func TestSmallMapDeleteMissing(t *testing.T) {
	var s SmallMap
	assert.False(t, s.Delete("nope"))
}

func TestSmallMapHas(t *testing.T) {
	var s SmallMap
	s.Set("x", 1)
	assert.True(t, s.Has("x"))
	assert.False(t, s.Has("y"))
}

func TestSmallMapPromote(t *testing.T) {
	var s SmallMap
	for i := 0; i < smallMapCap+5; i++ {
		s.Set(fmt.Sprintf("key%d", i), i)
	}
	assert.True(t, s.IsPromoted())
	assert.Equal(t, smallMapCap+5, s.Len())

	// Verify all keys readable.
	for i := 0; i < smallMapCap+5; i++ {
		v, ok := s.Get(fmt.Sprintf("key%d", i))
		require.True(t, ok)
		assert.Equal(t, i, v)
	}
}

func TestSmallMapToMapStr(t *testing.T) {
	var s SmallMap
	s.Set("a", 1)
	s.Set("b", "two")
	m := s.ToMapStr()
	assert.Equal(t, mapstr.M{"a": 1, "b": "two"}, m)
}

func TestSmallMapToMapStrEmpty(t *testing.T) {
	var s SmallMap
	assert.Nil(t, s.ToMapStr())
}

func TestSmallMapClone(t *testing.T) {
	var s SmallMap
	s.Set("a", 1)
	s.Set("b", 2)
	c := s.Clone()
	c.Set("a", 99)
	// Original unaffected.
	v, _ := s.Get("a")
	assert.Equal(t, 1, v)
	v, _ = c.Get("a")
	assert.Equal(t, 99, v)
}

func TestSmallMapClear(t *testing.T) {
	var s SmallMap
	for i := 0; i < 10; i++ {
		s.Set(fmt.Sprintf("k%d", i), i)
	}
	s.Clear()
	assert.Equal(t, 0, s.Len())
	assert.False(t, s.IsPromoted())
	_, ok := s.Get("k0")
	assert.False(t, ok)
}

func TestSmallMapClearPromoted(t *testing.T) {
	var s SmallMap
	for i := 0; i < smallMapCap+5; i++ {
		s.Set(fmt.Sprintf("k%d", i), i)
	}
	assert.True(t, s.IsPromoted())
	s.Clear()
	assert.Equal(t, 0, s.Len())
	assert.False(t, s.IsPromoted()) // map dropped
}

func TestSmallMapRange(t *testing.T) {
	var s SmallMap
	s.Set("a", 1)
	s.Set("b", 2)
	s.Set("c", 3)
	var keys []string
	s.Range(func(k string, v interface{}) bool {
		keys = append(keys, k)
		return true
	})
	assert.ElementsMatch(t, []string{"a", "b", "c"}, keys)
}

// --- Benchmarks ---

var smallMapSink interface{}

// BenchmarkSmallMap10SetGet: fill 10 keys, read them all back.
func BenchmarkSmallMap10SetGet(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var s SmallMap
		for _, k := range keys10 {
			s.Set(k, k)
		}
		for _, k := range keys10 {
			smallMapSink, _ = s.Get(k)
		}
	}
}

// BenchmarkGoMap10SetGet: same with plain Go map.
func BenchmarkGoMap10SetGet(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m := make(map[string]interface{}, 12)
		for _, k := range keys10 {
			m[k] = k
		}
		for _, k := range keys10 {
			smallMapSink = m[k]
		}
	}
}

// BenchmarkSmallMapPooled: pool a SmallMap, fill, read, clear, return.
func BenchmarkSmallMapPooled(b *testing.B) {
	pool := sync.Pool{
		New: func() interface{} {
			return &SmallMap{}
		},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := pool.Get().(*SmallMap)
		for _, k := range keys10 {
			s.Set(k, k)
		}
		for _, k := range keys10 {
			smallMapSink, _ = s.Get(k)
		}
		s.Clear()
		pool.Put(s)
	}
}

// BenchmarkGoMapPooled: same with pooled Go map + clear.
func BenchmarkGoMapPooled(b *testing.B) {
	pool := sync.Pool{
		New: func() interface{} {
			m := make(map[string]interface{}, 12)
			return &m
		},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mp := pool.Get().(*map[string]interface{})
		m := *mp
		for _, k := range keys10 {
			m[k] = k
		}
		for _, k := range keys10 {
			smallMapSink = m[k]
		}
		clear(m)
		pool.Put(mp)
	}
}

// BenchmarkSmallMapToMapStr: render to mapstr.M.
func BenchmarkSmallMapToMapStr(b *testing.B) {
	var s SmallMap
	for _, k := range keys10 {
		s.Set(k, k)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		smallMapSink = s.ToMapStr()
	}
}
