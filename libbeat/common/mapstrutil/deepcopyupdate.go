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

// Package mapstrutil provides utility functions for mapstr.M that are
// not yet available in the elastic-agent-libs/mapstr package.
//
// These functions will be upstreamed to elastic-agent-libs once
// elastic/elastic-agent-libs#390 is merged, at which point this
// package can be removed.
package mapstrutil

import (
	"github.com/elastic/elastic-agent-libs/mapstr"
)

// DeepCopyUpdate merges src into dst, creating fresh maps for nested
// values instead of aliasing src's sub-maps. This is equivalent to
// dst.DeepUpdate(src.Clone()) but performs both operations in a single
// pass, avoiding the intermediate clone allocation.
func DeepCopyUpdate(dst, src mapstr.M) {
	for k, v := range src {
		switch srcVal := v.(type) {
		case mapstr.M:
			if dstMap, ok := dst[k].(mapstr.M); ok {
				DeepCopyUpdate(dstMap, srcVal)
			} else {
				fresh := make(mapstr.M, len(srcVal))
				DeepCopyUpdate(fresh, srcVal)
				dst[k] = fresh
			}
		case map[string]interface{}:
			if dstMap, ok := dst[k].(mapstr.M); ok {
				DeepCopyUpdate(dstMap, mapstr.M(srcVal))
			} else {
				fresh := make(mapstr.M, len(srcVal))
				DeepCopyUpdate(fresh, mapstr.M(srcVal))
				dst[k] = fresh
			}
		default:
			dst[k] = v
		}
	}
}

// DeepCopyUpdateNoOverwrite merges src into dst like DeepCopyUpdate, but
// skips keys that already exist in the destination. Creates fresh nested
// maps without aliasing the source.
func DeepCopyUpdateNoOverwrite(dst, src mapstr.M) {
	for k, v := range src {
		switch srcVal := v.(type) {
		case mapstr.M:
			if dstMap, ok := dst[k].(mapstr.M); ok {
				DeepCopyUpdateNoOverwrite(dstMap, srcVal)
			} else if _, exists := dst[k]; !exists {
				fresh := make(mapstr.M, len(srcVal))
				DeepCopyUpdate(fresh, srcVal)
				dst[k] = fresh
			}
		case map[string]interface{}:
			if dstMap, ok := dst[k].(mapstr.M); ok {
				DeepCopyUpdateNoOverwrite(dstMap, mapstr.M(srcVal))
			} else if _, exists := dst[k]; !exists {
				fresh := make(mapstr.M, len(srcVal))
				DeepCopyUpdate(fresh, mapstr.M(srcVal))
				dst[k] = fresh
			}
		default:
			if _, exists := dst[k]; !exists {
				dst[k] = v
			}
		}
	}
}
