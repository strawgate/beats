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

package virtualmachine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

func TestEventMapping(t *testing.T) {
	var m MetricSet

	VirtualMachineTest := mo.VirtualMachine{
		ManagedEntity: mo.ManagedEntity{
			ExtensibleManagedObject: mo.ExtensibleManagedObject{
				Self: types.ManagedObjectReference{
					Value: "VM_0",
				},
			},
		},
		Summary: types.VirtualMachineSummary{
			OverallStatus: types.ManagedEntityStatus("green"),
			Config: types.VirtualMachineConfigSummary{
				Name:           "localhost.localdomain",
				GuestFullName:  "otherGuest",
				MemorySizeMB:   70,
				CpuReservation: 2294, // MHz
			},
			QuickStats: types.VirtualMachineQuickStats{
				UptimeSeconds:    10,
				OverallCpuUsage:  30, // MHz
				GuestMemoryUsage: 40, // MB
				HostMemoryUsage:  50, // MB
			},
		},
	}

	data := VMData{
		VM:             VirtualMachineTest,
		HostID:         "host-1234",
		HostName:       "test-host",
		NetworkNames:   []string{"network-1", "network-2"},
		DatastoreNames: []string{"ds1", "ds2"},
		CustomFields: mapstr.M{
			"customField1": "value1",
			"customField2": "value2",
		},
		Snapshots: []VMSnapshotData{
			{
				ID:          123,
				Name:        "Snapshot_1",
				Description: "Test snapshot 1",
				CreateTime:  common.Time{},
				State:       types.VirtualMachinePowerStatePoweredOff,
			},
			{
				ID:          745,
				Name:        "Snapshot_2",
				Description: "Test snapshot 2",
				CreateTime:  common.Time{},
				State:       types.VirtualMachinePowerStatePoweredOn,
			},
		},
	}

	event := m.mapEvent(data)

	// Expected event structure
	expectedEvent := mapstr.M{
		"name":   "localhost.localdomain",
		"os":     "otherGuest",
		"id":     "VM_0",
		"uptime": int32(10),
		"status": types.ManagedEntityStatus("green"),
		"cpu": mapstr.M{
			"used":  mapstr.M{"mhz": int32(30)},
			"total": mapstr.M{"mhz": int32(2294)},
			"free":  mapstr.M{"mhz": int32(2264)},
		},
		"memory": mapstr.M{
			"used": mapstr.M{
				"guest": mapstr.M{
					"bytes": int64(40 * 1024 * 1024),
				},
				"host": mapstr.M{
					"bytes": int64(50 * 1024 * 1024),
				},
			},
			"total": mapstr.M{
				"guest": mapstr.M{
					"bytes": int64(70 * 1024 * 1024),
				},
			},
			"free": mapstr.M{
				"guest": mapstr.M{
					"bytes": int64(30 * 1024 * 1024),
				},
			},
		},
		"host": mapstr.M{
			"id":       "host-1234",
			"hostname": "test-host",
		},
		"network": mapstr.M{
			"count": 2,
			"names": []string{"network-1", "network-2"},
		},
		"datastore": mapstr.M{
			"count": 2,
			"names": []string{"ds1", "ds2"},
		},
		"custom_fields": mapstr.M{
			"customField1": "value1",
			"customField2": "value2",
		},
		"network_names": []string{"network-1", "network-2"},
		"snapshot": mapstr.M{
			"info": []VMSnapshotData{
				{
					ID:          123,
					Name:        "Snapshot_1",
					Description: "Test snapshot 1",
					CreateTime:  common.Time{},
					State:       types.VirtualMachinePowerStatePoweredOff,
				},
				{
					ID:          745,
					Name:        "Snapshot_2",
					Description: "Test snapshot 2",
					CreateTime:  common.Time{},
					State:       types.VirtualMachinePowerStatePoweredOn,
				},
			},
			"count": 2,
		},
	}

	// Assert that the output event matches the expected event
	assert.Exactly(t, expectedEvent, event)

}
