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

package instance

import (
	"bufio"
	"os"
	"runtime"
	"strings"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

// logMemoryInfo reads /proc/self/smaps_rollup (or falls back to /proc/self/status)
// and logs memory breakdown including private vs shared RSS. This helps diagnose
// startup memory usage, especially the portion invisible to Go's heap profiler.
func logMemoryInfo(log *logp.Logger) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	goMem := mapstr.M{
		"heap_alloc_bytes":   memStats.HeapAlloc,
		"heap_sys_bytes":     memStats.HeapSys,
		"heap_objects":       memStats.HeapObjects,
		"stack_sys_bytes":    memStats.StackSys,
		"gc_sys_bytes":       memStats.GCSys,
		"total_alloc_bytes":  memStats.TotalAlloc,
		"total_sys_bytes":    memStats.Sys,
		"num_goroutines":     runtime.NumGoroutine(),
		"num_gc":             memStats.NumGC,
	}

	// Try smaps_rollup first (aggregated, fast), fall back to status
	procMem := readSmapsRollup()
	if procMem == nil {
		procMem = readProcStatus()
	}

	info := mapstr.M{"go": goMem}
	if procMem != nil {
		info["proc"] = procMem
	}

	log.Infow("Startup memory info", "memory", info)
}

// readSmapsRollup reads /proc/self/smaps_rollup for aggregated memory info.
// Available on Linux 4.14+.
func readSmapsRollup() mapstr.M {
	f, err := os.Open("/proc/self/smaps_rollup")
	if err != nil {
		return nil
	}
	defer f.Close()

	result := mapstr.M{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		switch key {
		case "Rss":
			result["rss_kb"] = parts[1]
		case "Pss":
			result["pss_kb"] = parts[1]
		case "Shared_Clean":
			result["shared_clean_kb"] = parts[1]
		case "Shared_Dirty":
			result["shared_dirty_kb"] = parts[1]
		case "Private_Clean":
			result["private_clean_kb"] = parts[1]
		case "Private_Dirty":
			result["private_dirty_kb"] = parts[1]
		case "Anonymous":
			result["anonymous_kb"] = parts[1]
		}
	}
	return result
}

// readProcStatus reads VmRSS and VmSize from /proc/self/status as a fallback.
func readProcStatus() mapstr.M {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return nil
	}
	defer f.Close()

	result := mapstr.M{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		switch key {
		case "VmRSS":
			result["rss_kb"] = parts[1]
		case "VmSize":
			result["vm_size_kb"] = parts[1]
		case "RssAnon":
			result["rss_anon_kb"] = parts[1]
		case "RssFile":
			result["rss_file_kb"] = parts[1]
		case "RssShmem":
			result["rss_shmem_kb"] = parts[1]
		}
	}
	return result
}
