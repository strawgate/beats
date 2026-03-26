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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/mapstr"
)

func TestEvent(t *testing.T) {
	metadataNestedNestedMap := mapstr.M{
		"metaLevel2Value": "metavalue3",
	}
	metadataNestedMap := mapstr.M{
		"metaLevel1Map": metadataNestedNestedMap,
	}

	fieldsNestedNestedMap := mapstr.M{
		"fieldsLevel2Value": "fieldsvalue3",
	}
	fieldsNestedMap := mapstr.M{
		"fieldsLevel1Map": fieldsNestedNestedMap,
	}

	metaUntouchedMap := mapstr.M{}
	fieldsUntouchedMap := mapstr.M{}

	event := &Event{
		Timestamp: time.Now(),
		Meta: mapstr.M{
			"a.b":             "c",
			"metaLevel0Map":   metadataNestedMap,
			"metaLevel0Value": "metavalue1",
			// these keys should never be edited by the tests
			// to verify that existing keys remain
			"metaLevel0Value2": "untouched",
			"metaUntouchedMap": metaUntouchedMap,
		},
		Fields: mapstr.M{
			"a.b":               "c",
			"fieldsLevel0Map":   fieldsNestedMap,
			"fieldsLevel0Value": "fieldsvalue1",
			// these keys should never be edited by the tests
			// to verify that existing keys remain
			"fieldsLevel0Value2": "untouched",
			"fieldsUntouchedMap": fieldsUntouchedMap,
		},
	}

	t.Run("empty", func(t *testing.T) {
		t.Run("Delete", func(t *testing.T) {
			event := &Event{}
			require.NotPanics(t, func() {
				err := event.Delete(metadataKeyPrefix + "some")
				require.Error(t, err)
				require.ErrorIs(t, err, mapstr.ErrKeyNotFound)
				err = event.Delete("some")
				require.Error(t, err)
				require.ErrorIs(t, err, mapstr.ErrKeyNotFound)
			})
		})

		t.Run("HasKey", func(t *testing.T) {
			event := &Event{}
			require.NotPanics(t, func() {
				has, err := event.HasKey(metadataKeyPrefix + "some")
				require.NoError(t, err)
				require.False(t, has)
				has, err = event.HasKey("some")
				require.NoError(t, err)
				require.False(t, has)
			})
		})

		t.Run("GetValue", func(t *testing.T) {
			event := &Event{}
			require.NotPanics(t, func() {
				_, err := event.GetValue(metadataKeyPrefix + "some")
				require.Error(t, err)
				require.ErrorIs(t, err, mapstr.ErrKeyNotFound)
				_, err = event.GetValue("some")
				require.Error(t, err)
				require.ErrorIs(t, err, mapstr.ErrKeyNotFound)
			})
		})

		t.Run("PutValue", func(t *testing.T) {
			event := &Event{}
			require.NotPanics(t, func() {
				prev, err := event.PutValue(metadataKeyPrefix+"some", "value")
				require.NoError(t, err)
				require.Nil(t, prev)
				prev, err = event.PutValue("some", "value")
				require.NoError(t, err)
				require.Nil(t, prev)
			})
		})

		t.Run("DeepUpdate", func(t *testing.T) {
			event := &Event{}
			require.NotPanics(t, func() {
				event.DeepUpdate(mapstr.M{
					MetadataFieldKey: mapstr.M{"key": "value"},
					"key":            "value",
				})
			})
		})

		t.Run("String", func(t *testing.T) {
			event := &Event{}
			require.NotPanics(t, func() {
				s := event.String()
				require.Equal(t, `{"@metadata":{},"@timestamp":"0001-01-01T00:00:00Z"}`, s)
			})
		})
	})

	t.Run("Get", func(t *testing.T) {
		cases := []struct {
			name   string
			key    string
			exp    interface{}
			expErr error
		}{
			{
				name: TimestampFieldKey,
				key:  TimestampFieldKey,
				exp:  event.Timestamp,
			},
			{
				name:   "no acess to metadata key",
				key:    MetadataFieldKey,
				expErr: ErrMetadataAccess,
			},
			{
				name:   "non-existing metadata sub-key",
				key:    metadataKeyPrefix + "none",
				expErr: mapstr.ErrKeyNotFound,
			},
			{
				name: "a value type from metadata",
				key:  metadataKeyPrefix + "metaLevel0Value",
				exp:  "metavalue1",
			},
			{
				name: "a root-level dot-key from metadata",
				key:  metadataKeyPrefix + "a.b",
				exp:  "c",
			},
			{
				name: "a nested map from metadata",
				key:  metadataKeyPrefix + "metaLevel0Map",
				exp:  metadataNestedMap,
			},
			{
				name:   "non-existing field key",
				key:    "none",
				expErr: mapstr.ErrKeyNotFound,
			},
			{
				name: "a value type from fields",
				key:  "fieldsLevel0Value",
				exp:  "fieldsvalue1",
			},
			{
				name: "a root-level dot-key from fields",
				key:  "a.b",
				exp:  "c",
			},
			{
				name: "a nested map from fields",
				key:  "fieldsLevel0Map",
				exp:  fieldsNestedMap,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				val, err := event.GetValue(tc.key)
				if tc.expErr != nil {
					require.Error(t, err)
					require.Nil(t, val)
					require.ErrorIs(t, err, tc.expErr)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.exp, val)
			})
		}
	})

	t.Run("Delete", func(t *testing.T) {
		cases := []struct {
			name   string
			key    string
			exp    interface{}
			expErr error
		}{
			{
				name:   TimestampFieldKey,
				key:    TimestampFieldKey,
				expErr: ErrDeleteTimestamp,
			},
			{
				name:   "no acess to metadata key",
				key:    MetadataFieldKey,
				expErr: ErrAlterMetadataKey,
			},
			{
				name:   "non-existing metadata sub key",
				key:    metadataKeyPrefix + "none",
				expErr: mapstr.ErrKeyNotFound,
			},
			{
				name: "a value type from metadata",
				key:  metadataKeyPrefix + "metaLevel0Value",
				exp:  "metavalue1",
			},
			{
				name: "a root-level dot-key from metadata",
				key:  metadataKeyPrefix + "a.b",
				exp:  "c",
			},
			{
				name: "a nested map from metadata",
				key:  metadataKeyPrefix + "metaLevel0Map",
				exp:  metadataNestedMap,
			},
			{
				name:   "non-existing field key",
				key:    "none",
				expErr: mapstr.ErrKeyNotFound,
			},
			{
				name: "a value type from fields",
				key:  "fieldsLevel0Value",
				exp:  "fieldsvalue1",
			},
			{
				name: "a root-level dot-key from fields",
				key:  "a.b",
				exp:  "c",
			},
			{
				name: "a nested map from fields",
				key:  "fieldsLevel0Map",
				exp:  fieldsNestedMap,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				event := event.Clone()
				err := event.Delete(tc.key)
				if tc.expErr != nil {
					require.Error(t, err)
					require.ErrorIs(t, err, tc.expErr)
					return
				}
				require.NoError(t, err)
				_, err = event.GetValue(tc.key)
				require.Error(t, err)
				require.ErrorIs(t, err, mapstr.ErrKeyNotFound)
			})
		}
	})

	t.Run("PutValue", func(t *testing.T) {
		newTs := time.Now().Add(time.Hour)
		cases := []struct {
			name    string
			key     string
			val     interface{}
			expPrev interface{}
			expErr  error
		}{
			{
				name:    "timestamp",
				key:     TimestampFieldKey,
				val:     newTs,
				expPrev: event.Timestamp,
			},
			{
				name:   "incorrect type for timestamp",
				key:    TimestampFieldKey,
				val:    "wrong",
				expErr: ErrValueNotTimestamp,
			},
			{
				name:   "no acess to metadata key",
				key:    MetadataFieldKey,
				expErr: ErrAlterMetadataKey,
			},
			{
				name:    "non-existing metadata key",
				key:     metadataKeyPrefix + "none",
				expPrev: nil,
			},
			{
				name:    "a value type from metadata",
				key:     metadataKeyPrefix + "metaLevel0Value",
				val:     "some",
				expPrev: "metavalue1",
			},
			{
				name:    "a root-level dot-key from metadata",
				key:     metadataKeyPrefix + "a.b",
				val:     "d",
				expPrev: "c",
			},
			{
				name:    "a nested map from metadata",
				key:     metadataKeyPrefix + "metaLevel0Map",
				val:     "some",
				expPrev: metadataNestedMap,
			},
			{
				name:    "non-existing field key",
				key:     "none",
				val:     "some",
				expPrev: nil,
			},
			{
				name:    "a value type from fields",
				key:     "fieldsLevel0Value",
				val:     "some",
				expPrev: "fieldsvalue1",
			},
			{
				name:    "a root-level dot-key from fields",
				key:     "a.b",
				val:     "d",
				expPrev: "c",
			},
			{
				name:    "a nested map from fields",
				key:     "fieldsLevel0Map",
				val:     "some",
				expPrev: fieldsNestedMap,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				event := event.Clone()
				prevVal, err := event.PutValue(tc.key, tc.val)
				if tc.expErr != nil {
					require.Error(t, err)
					require.ErrorIs(t, err, tc.expErr)
					require.Nil(t, prevVal)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.expPrev, prevVal)
				actual, err := event.GetValue(tc.key)
				require.NoError(t, err)
				require.Equal(t, tc.val, actual)
				has, err := event.HasKey(tc.key)
				require.NoError(t, err)
				require.True(t, has)
			})
		}

		t.Run("type conflict", func(t *testing.T) {
			event := &Event{
				Meta: mapstr.M{
					"a": 9,
					"c": 10,
				},
				Fields: mapstr.M{
					"a": 9,
					"c": 10,
				},
			}

			_, err := event.PutValue("a.c", 10)
			require.Error(t, err)
			require.Equal(t, "expected map but type is int", err.Error())
			_, err = event.PutValue("a.value", 9)
			require.Error(t, err)
			require.Equal(t, "expected map but type is int", err.Error())
		})

		t.Run("hierarchy", func(t *testing.T) {
			event := &Event{
				Fields: mapstr.M{
					"a.b": 1,
				},
			}
			err := event.Delete("a.b")
			require.NoError(t, err)

			prev, err := event.PutValue("a.b.c", 1)
			require.NoError(t, err)
			require.Nil(t, prev)

			expFields := mapstr.M{
				"a": mapstr.M{
					"b": mapstr.M{
						"c": 1,
					},
				},
			}

			require.Equal(t, expFields, event.Fields)
		})

		t.Run("SetID", func(t *testing.T) {
			event := &Event{}
			event.SetID("unique")
			require.Equal(t, "unique", event.Meta["_id"])
		})
	})

	t.Run("SetErrorWithOption", func(t *testing.T) {
		cloned := event.Clone()
		cloned.SetErrorWithOption("message", false, "data", "field")
		require.Equal(t, event, cloned)
		expEvent := cloned.Clone()
		expEvent.Fields[ErrorFieldKey] = mapstr.M{
			"message": "message",
			"field":   "field",
			"data":    "data",
			"type":    "json",
		}
		cloned.SetErrorWithOption("message", true, "data", "field")
		require.Equal(t, expEvent, cloned)
	})

	t.Run("DeepUpdate", func(t *testing.T) {
		newTs := time.Now().Add(time.Hour)
		update := map[string]interface{}{
			TimestampFieldKey: newTs,
			MetadataFieldKey: map[string]interface{}{
				"metaLevel0Map": mapstr.M{ // mix types on purpose, should support both
					"metaLevel1Map": map[string]interface{}{
						"new1": "newmetavalue1",
					},
				},
				"metaLevel0Value": "metareplaced1",
				"new2":            "newmetavalue2",
			},
			"fieldsLevel0Map": map[string]interface{}{
				"fieldsLevel1Map": mapstr.M{
					"new3": "newfieldsvalue1",
				},
				"newmap": map[string]interface{}{
					"new4": "newfieldsvalue2",
				},
			},
			"fieldsLevel0Value": "fieldsreplaced1",
		}

		t.Run("empty", func(t *testing.T) {
			cloned := event.Clone()
			cloned.DeepUpdate(nil)
			require.Equal(t, event.Meta, cloned.Meta)
			require.Equal(t, event.Fields, cloned.Fields)
		})

		t.Run("overwrite", func(t *testing.T) {
			event := event.Clone()
			event.DeepUpdate(update)

			expEvent := &Event{
				Timestamp: newTs,
				Meta: mapstr.M{
					"a.b": "c",
					"metaLevel0Map": mapstr.M{
						"metaLevel1Map": mapstr.M{
							"metaLevel2Value": "metavalue3",
							"new1":            "newmetavalue1",
						},
					},
					"metaLevel0Value":  "metareplaced1",
					"metaLevel0Value2": "untouched",
					"new2":             "newmetavalue2",
					"metaUntouchedMap": metaUntouchedMap,
				},
				Fields: mapstr.M{
					"a.b": "c",
					"fieldsLevel0Map": mapstr.M{
						"fieldsLevel1Map": mapstr.M{
							"fieldsLevel2Value": "fieldsvalue3",
							"new3":              "newfieldsvalue1",
						},
						"newmap": mapstr.M{
							"new4": "newfieldsvalue2",
						},
					},
					"fieldsLevel0Value":  "fieldsreplaced1",
					"fieldsLevel0Value2": "untouched",
					"fieldsUntouchedMap": fieldsUntouchedMap,
				},
			}

			require.Equal(t, expEvent.Timestamp, event.Timestamp)
			require.Equal(t, expEvent.Meta, event.Meta)
			require.Equal(t, expEvent.Fields, event.Fields)
		})

		t.Run("no overwrite", func(t *testing.T) {
			cloned := event.Clone()
			cloned.DeepUpdateNoOverwrite(update)

			expEvent := &Event{
				// should have the original/non-overwritten timestamp value
				Timestamp: event.Timestamp,
				Meta: mapstr.M{
					"a.b": "c",
					"metaLevel0Map": mapstr.M{
						"metaLevel1Map": mapstr.M{
							"metaLevel2Value": "metavalue3",
							"new1":            "newmetavalue1",
						},
					},
					"metaLevel0Value":  "metavalue1",
					"metaLevel0Value2": "untouched",
					"new2":             "newmetavalue2",
					"metaUntouchedMap": metaUntouchedMap,
				},
				Fields: mapstr.M{
					"a.b": "c",
					"fieldsLevel0Map": mapstr.M{
						"fieldsLevel1Map": mapstr.M{
							"fieldsLevel2Value": "fieldsvalue3",
							"new3":              "newfieldsvalue1",
						},
						"newmap": mapstr.M{
							"new4": "newfieldsvalue2",
						},
					},
					"fieldsLevel0Value":  "fieldsvalue1",
					"fieldsLevel0Value2": "untouched",
					"fieldsUntouchedMap": fieldsUntouchedMap,
				},
			}

			require.Equal(t, expEvent.Timestamp, cloned.Timestamp)
			require.Equal(t, expEvent.Meta, cloned.Meta)
			require.Equal(t, expEvent.Fields, cloned.Fields)
		})

		t.Run("fast path - no special keys", func(t *testing.T) {
			// This exercises the deepUpdate fast path where neither @timestamp
			// nor @metadata exist in the update map.
			event := &Event{
				Timestamp: time.Now(),
				Meta:      mapstr.M{"existing_meta": "preserved"},
				Fields: mapstr.M{
					"existing": "value",
					"nested":   mapstr.M{"a": "1"},
				},
			}

			update := mapstr.M{
				"new_field": "added",
				"nested":    mapstr.M{"b": "2"},
			}

			origTimestamp := event.Timestamp
			event.DeepUpdate(update)

			// Timestamp must not be changed
			require.Equal(t, origTimestamp, event.Timestamp)
			// Meta must not be changed
			require.Equal(t, mapstr.M{"existing_meta": "preserved"}, event.Meta)
			// Fields must be merged
			require.Equal(t, "value", event.Fields["existing"])
			require.Equal(t, "added", event.Fields["new_field"])
			// Nested merge
			nested := event.Fields["nested"].(mapstr.M)
			require.Equal(t, "1", nested["a"])
			require.Equal(t, "2", nested["b"])
		})

		t.Run("fast path - nil fields", func(t *testing.T) {
			event := &Event{Fields: nil}
			event.DeepUpdate(mapstr.M{"key": "value"})
			require.Equal(t, mapstr.M{"key": "value"}, event.Fields)
		})

		t.Run("fast path no-overwrite - no special keys", func(t *testing.T) {
			event := &Event{
				Fields: mapstr.M{
					"existing": "original",
					"nested":   mapstr.M{"a": "1"},
				},
			}

			update := mapstr.M{
				"existing":  "should-not-overwrite",
				"new_field": "added",
				"nested":    mapstr.M{"a": "should-not-overwrite", "b": "2"},
			}

			event.DeepUpdateNoOverwrite(update)

			require.Equal(t, "original", event.Fields["existing"])
			require.Equal(t, "added", event.Fields["new_field"])
			nested := event.Fields["nested"].(mapstr.M)
			require.Equal(t, "1", nested["a"])
			require.Equal(t, "2", nested["b"])
		})

		t.Run("update map not mutated", func(t *testing.T) {
			// Verify the input map is restored after deepUpdate even when
			// it contains @timestamp and @metadata
			ts := time.Now()
			update := mapstr.M{
				TimestampFieldKey: ts,
				MetadataFieldKey:  mapstr.M{"key": "value"},
				"regular":         "field",
			}
			updateCopy := update.Clone()

			event := &Event{Fields: mapstr.M{}}
			event.DeepUpdate(update)

			require.Equal(t, updateCopy, update,
				"update map was mutated by DeepUpdate")
		})
	})

	t.Run("String", func(t *testing.T) {
		ts := time.Now().Add(time.Hour)
		event := &Event{
			Timestamp: ts,
			Meta: mapstr.M{
				"metakey": "metavalue",
			},
			Fields: mapstr.M{
				"key": "value",
			},
		}

		exp := mapstr.M{
			TimestampFieldKey: ts,
			MetadataFieldKey: mapstr.M{
				"metakey": "metavalue",
			},
			"key": "value",
		}

		require.Equal(t, exp.String(), event.String())
	})
}

// BenchmarkEventDeepUpdate_NoSpecialKeys benchmarks the fast path where
// the update map contains no @timestamp or @metadata keys.
func BenchmarkEventDeepUpdate_NoSpecialKeys(b *testing.B) {
	fields := mapstr.M{
		"elastic_agent": mapstr.M{
			"id":       "agent-123",
			"snapshot": false,
			"version":  "8.12.0",
		},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := &Event{
			Timestamp: time.Now(),
			Fields: mapstr.M{
				"message": "test",
				"host":    mapstr.M{"name": "testhost"},
			},
		}
		event.DeepUpdate(fields)
	}
}

// BenchmarkEventDeepUpdate_WithSpecialKeys benchmarks the slow path where
// the update map contains @timestamp and @metadata keys.
func BenchmarkEventDeepUpdate_WithSpecialKeys(b *testing.B) {
	ts := time.Now()
	fields := mapstr.M{
		TimestampFieldKey: ts,
		MetadataFieldKey:  mapstr.M{"input_id": "test"},
		"elastic_agent": mapstr.M{
			"id": "agent-123",
		},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := &Event{
			Timestamp: time.Now(),
			Fields: mapstr.M{
				"message": "test",
				"host":    mapstr.M{"name": "testhost"},
			},
		}
		event.DeepUpdate(fields)
	}
}

// BenchmarkEventDeepUpdateNoOverwrite_NoSpecialKeys benchmarks the fast path
// for DeepUpdateNoOverwrite.
func BenchmarkEventDeepUpdateNoOverwrite_NoSpecialKeys(b *testing.B) {
	fields := mapstr.M{
		"agent": mapstr.M{"id": "agent-123"},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event := &Event{
			Fields: mapstr.M{
				"agent": mapstr.M{"id": "existing", "type": "filebeat"},
			},
		}
		event.DeepUpdateNoOverwrite(fields)
	}
}
