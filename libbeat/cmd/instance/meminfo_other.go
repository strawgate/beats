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

//go:build !linux

package instance

import (
	"runtime"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

// logMemoryInfo logs Go runtime memory stats on non-Linux platforms.
// /proc/self/smaps is not available, so only Go-level stats are reported.
func logMemoryInfo(log *logp.Logger) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	goMem := mapstr.M{
		"heap_alloc_bytes":  memStats.HeapAlloc,
		"heap_sys_bytes":    memStats.HeapSys,
		"heap_objects":      memStats.HeapObjects,
		"stack_sys_bytes":   memStats.StackSys,
		"gc_sys_bytes":      memStats.GCSys,
		"total_alloc_bytes": memStats.TotalAlloc,
		"total_sys_bytes":   memStats.Sys,
		"num_goroutines":    runtime.NumGoroutine(),
		"num_gc":            memStats.NumGC,
	}

	log.Infow("Startup memory info", "memory", mapstr.M{"go": goMem})
}
