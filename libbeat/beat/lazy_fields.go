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
	"sync"

	"github.com/elastic/beats/v7/libbeat/asset"
)

// LazyFields provides lazy loading of beat field definitions (fields.yml).
// Field data is only decompressed from the asset registry on first access,
// reducing startup memory usage for beats that don't immediately need
// their field definitions (e.g. during normal "run" mode).
type LazyFields struct {
	once sync.Once
	data []byte
	err  error
	name string // beat name for asset.GetFields lookup
}

// NewLazyFields creates a LazyFields that will load field data for the
// given beat name on first access.
func NewLazyFields(beatName string) *LazyFields {
	return &LazyFields{name: beatName}
}

// NewLazyFieldsFromData creates a LazyFields pre-loaded with the given data.
// This is useful for tests that need to provide field data directly.
func NewLazyFieldsFromData(data []byte) *LazyFields {
	lf := &LazyFields{data: data}
	lf.once.Do(func() {}) // mark as already loaded
	return lf
}

// Get returns the decompressed field definition data. The data is loaded
// and cached on first call. Subsequent calls return the cached result.
func (lf *LazyFields) Get() ([]byte, error) {
	lf.once.Do(func() {
		lf.data, lf.err = asset.GetFields(lf.name)
		// Clean up the registry entries for this beat since we've cached the
		// decompressed result. This frees the compressed string references
		// and nested map structures in FieldsRegistry.
		asset.CleanupRegistry(lf.name)
	})
	return lf.data, lf.err
}
