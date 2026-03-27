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
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

type updateMode bool

var (
	updateModeOverwrite   updateMode = true
	updateModeNoOverwrite updateMode = false
)

// FlagField fields used to keep information or errors when events are parsed.
const FlagField = "log.flags"

const (
	TimestampFieldKey = "@timestamp"
	MetadataFieldKey  = "@metadata"
	ErrorFieldKey     = "error"
	metadataKeyPrefix = MetadataFieldKey + "."
	metadataKeyOffset = len(metadataKeyPrefix)
)

// Event is the common event format shared by all beats.
// Every event must have a timestamp and provide encodable Fields in `Fields`.
// The `Meta`-fields can be used to pass additional meta-data to the outputs.
// Output can optionally publish a subset of Meta, or ignore Meta.
// maxInlineFields is the fixed capacity of the inline field entries array.
// Sized for a typical event: message, cloud, host, agent, elastic_agent,
// data_stream, event, ecs, plus a few input-specific fields.
const maxInlineFields = 16

// fieldEntry is a key-value pair stored in the inline fields array.
type fieldEntry struct {
	key   string
	value interface{}
}

type Event struct {
	Timestamp time.Time
	Meta      mapstr.M
	fields    mapstr.M // cached materialized view — use Fields() to access

	// Private is for input-specific data. The input that populates this field
	// is fully responsible for its management. No guarantees are given about
	// the content of this field as other components are able to modify it.
	Private    interface{}
	TimeSeries bool // true if the event contains timeseries data

	// Inline field storage — avoids map allocation for top-level keys.
	// Used by PutValueQuiet/cowField for fast O(n) access on small N.
	// When nFields > 0, this is the source of truth; fields is populated
	// by Materialize() before encoding.
	entries  [maxInlineFields]fieldEntry
	nFields  int
	hasCow   bool // true when entries contains at least one cowMap value
	overflow mapstr.M // spillover for events with > maxInlineFields top-level keys
}

// inlineGet looks up a top-level key in the inline entries array.
func (e *Event) inlineGet(key string) (interface{}, bool) {
	for i := 0; i < e.nFields; i++ {
		if e.entries[i].key == key {
			return e.entries[i].value, true
		}
	}
	if e.overflow != nil {
		v, ok := e.overflow[key]
		return v, ok
	}
	return nil, false
}

// inlineSet sets a top-level key in the inline entries array.
// Returns the old value if the key already existed.
func (e *Event) inlineSet(key string, value interface{}) (interface{}, bool) {
	for i := 0; i < e.nFields; i++ {
		if e.entries[i].key == key {
			old := e.entries[i].value
			e.entries[i].value = value
			return old, true
		}
	}
	if e.overflow != nil {
		if old, ok := e.overflow[key]; ok {
			e.overflow[key] = value
			return old, true
		}
	}
	// New key.
	if e.nFields < maxInlineFields {
		e.entries[e.nFields] = fieldEntry{key: key, value: value}
		e.nFields++
	} else {
		if e.overflow == nil {
			e.overflow = make(mapstr.M, 4)
		}
		e.overflow[key] = value
	}
	return nil, false
}

// inlineDelete removes a top-level key from the inline entries array.
func (e *Event) inlineDelete(key string) bool {
	for i := 0; i < e.nFields; i++ {
		if e.entries[i].key == key {
			// Shift remaining entries down.
			copy(e.entries[i:], e.entries[i+1:e.nFields])
			e.nFields--
			e.entries[e.nFields] = fieldEntry{} // clear last slot
			return true
		}
	}
	if e.overflow != nil {
		if _, ok := e.overflow[key]; ok {
			delete(e.overflow, key)
			return true
		}
	}
	return false
}

// Fields returns the event's fields as a mapstr.M.
// This renders the internal inline entries into a map, unwrapping
// any cowMap values. The returned map is safe for read-only use
// (e.g., encoding). Callers that need a mutable copy should use
// GetValue/PutValue methods or call Clone().
func (e *Event) Fields() mapstr.M {
	e.Materialize()
	return e.fields
}

// SetFields sets the event's fields from a mapstr.M.
// Used during event creation to initialize fields.
func (e *Event) SetFields(m mapstr.M) {
	e.fields = m
	// Also populate inline entries from the map.
	for k, v := range m {
		e.inlineSet(k, v)
	}
}

var eventPool = sync.Pool{
	New: func() interface{} {
		return &Event{}
	},
}

// NewEvent returns an Event from the pool. The inline entries array
// is zeroed and ready for use — no map allocation needed.
func NewEvent() *Event {
	e := eventPool.Get().(*Event) //nolint:errcheck
	return e
}

// ReleaseEvent returns an Event to the pool for reuse.
// The Event must not be referenced after this call.
func ReleaseEvent(e *Event) {
	// Clear inline entries to release references for GC.
	for i := 0; i < e.nFields; i++ {
		e.entries[i] = fieldEntry{}
	}
	e.nFields = 0
	e.hasCow = false
	e.overflow = nil
	e.fields = nil
	e.Timestamp = time.Time{}
	e.Meta = nil
	e.Private = nil
	e.TimeSeries = false
	eventPool.Put(e)
}

var (
	ErrValueNotTimestamp = errors.New("value is not a timestamp")
	ErrValueNotMapStr    = errors.New("value is not `mapstr.M` or `map[string]interface{}` type")
	ErrAlterMetadataKey  = fmt.Errorf("deleting/replacing %q key is not supported", MetadataFieldKey)
	ErrMetadataAccess    = fmt.Errorf("accessing %q key directly is not supported, try nested keys", MetadataFieldKey)
	ErrDeleteTimestamp   = fmt.Errorf("deleting %q key is not supported", TimestampFieldKey)
)

// SetID overwrites the "id" field in the events metadata.
// If Meta is nil, a new Meta dictionary is created.
func (e *Event) SetID(id string) {
	_, _ = e.PutValue(metadataKeyPrefix+"_id", id)
}

// GetValue gets a value from the event. If the key does not exist then an error
// is returned.
//
// Use `@timestamp` key for getting the event timestamp.
// Use `@metadata.*` keys for getting the event metadata fields.
// If `@metadata` key is used then `ErrMetadataAccess` is returned.
func (e *Event) GetValue(key string) (interface{}, error) {
	if key == TimestampFieldKey {
		return e.Timestamp, nil
	}
	if key == MetadataFieldKey {
		return nil, ErrMetadataAccess
	}

	if subKey, ok := e.metadataSubKey(key); ok {
		if e.Meta == nil {
			return nil, mapstr.ErrKeyNotFound
		}
		return e.Meta.GetValue(subKey)
	}

	if e.fields == nil {
		return nil, mapstr.ErrKeyNotFound
	}

	if _, subKey, cm := e.cowField(key); cm != nil {
		if subKey == "" {
			// Return a clone so callers can't corrupt shared data.
			return cm.shared.Clone(), nil
		}
		v, err := cm.shared.GetValue(subKey)
		if err != nil {
			return v, err
		}
		// Clone map values to prevent shared data corruption.
		switch m := v.(type) {
		case mapstr.M:
			return m.Clone(), nil
		case map[string]interface{}:
			return mapstr.M(m).Clone(), nil
		default:
			return v, nil
		}
	}

	return e.fields.GetValue(key)
}

// Clone creates an exact copy of the event
func (e *Event) Clone() *Event {
	c := &Event{
		Timestamp:  e.Timestamp,
		Meta:       e.Meta.Clone(),
		Private:    e.Private,
		TimeSeries: e.TimeSeries,
	}
	c.fields = e.cloneFields()
	c.hasCow = e.hasCow
	return c
}

// DeepUpdate recursively copies the key-value pairs from `d` to various properties of the event.
// When the key equals `@timestamp` it's set as the `Timestamp` property of the event.
// When the key equals `@metadata` the update is routed into the `Meta` map instead of `Fields`
// The rest of the keys are set to the `Fields` map.
// If the key is present and the value is a map as well, the sub-map will be updated recursively
// via `DeepUpdate`.
// `DeepUpdateNoOverwrite` is a version of this function that does not
// overwrite existing values.
func (e *Event) DeepUpdate(d mapstr.M) {
	e.deepUpdate(d, updateModeOverwrite)
}

// DeepUpdateNoOverwrite recursively copies the key-value pairs from `d` to various properties of the event.
// The `@timestamp` update is ignored due to "no overwrite" behavior.
// When the key equals `@metadata` the update is routed into the `Meta` map instead of `Fields`.
// The rest of the keys are set to the `Fields` map.
// If the key is present and the value is a map as well, the sub-map will be updated recursively
// via `DeepUpdateNoOverwrite`.
// `DeepUpdate` is a version of this function that overwrites existing values.
func (e *Event) DeepUpdateNoOverwrite(d mapstr.M) {
	e.deepUpdate(d, updateModeNoOverwrite)
}

func (e *Event) deepUpdate(d mapstr.M, mode updateMode) {
	if len(d) == 0 {
		return
	}

	// It's supported to update the timestamp using this function.
	// However, we must handle it separately since it's a separate field of the event.
	timestampValue, timestampExists := d[TimestampFieldKey]
	if timestampExists {
		if mode == updateModeOverwrite {
			_, _ = e.setTimestamp(timestampValue)
		}

		// Temporary delete it from the update map,
		// so we can do `e.fields.DeepUpdate(d)` or
		// `e.fields.DeepUpdateNoOverwrite(d)` later
		delete(d, TimestampFieldKey)
		defer func() {
			d[TimestampFieldKey] = timestampValue
		}()
	}

	// It's supported to update the metadata using this function.
	// However, we must handle it separately since it's a separate field of the event.
	metaValue, metaExists := d[MetadataFieldKey]
	if metaExists {
		var metaUpdate mapstr.M

		switch meta := metaValue.(type) {
		case mapstr.M:
			metaUpdate = meta
		case map[string]interface{}:
			metaUpdate = mapstr.M(meta)
		}

		if metaUpdate != nil {
			if e.Meta == nil {
				e.Meta = mapstr.M{}
			}
			switch mode {
			case updateModeOverwrite:
				e.Meta.DeepUpdate(metaUpdate)
			case updateModeNoOverwrite:
				e.Meta.DeepUpdateNoOverwrite(metaUpdate)
			}
		}

		// Temporary delete it from the update map,
		// so we can do `e.fields.DeepUpdate(d)` or
		// `e.fields.DeepUpdateNoOverwrite(d)` later
		delete(d, MetadataFieldKey)
		defer func() {
			d[MetadataFieldKey] = metaValue
		}()
	}

	if len(d) == 0 {
		return
	}

	if e.fields == nil {
		e.fields = mapstr.M{}
	}

	// Materialize any cowMaps that overlap with map-valued update keys,
	// so DeepUpdate can merge into them correctly.
	e.materializeCowsForUpdate(d)

	switch mode {
	case updateModeOverwrite:
		e.fields.DeepUpdate(d)
	case updateModeNoOverwrite:
		e.fields.DeepUpdateNoOverwrite(d)
	}
}

func (e *Event) setTimestamp(v interface{}) (interface{}, error) {
	// to satisfy the PutValue interface, this function
	// must return the overwritten value
	prevValue := e.Timestamp

	switch ts := v.(type) {
	case time.Time:
		e.Timestamp = ts
		return prevValue, nil
	case common.Time:
		e.Timestamp = time.Time(ts)
		return prevValue, nil
	default:
		return nil, ErrValueNotTimestamp
	}
}

// Put associates the specified value with the specified key. If the event
// previously contained a mapping for the key, the old value is replaced and
// returned. The key can be expressed in dot-notation (e.g. x.y) to put a value
// into a nested map.
//
// If you need insert keys containing dots then you must use bracket notation
// to insert values (e.g. m[key] = value).
//
// Use `@timestamp` key for setting the event timestamp.
// Use `@metadata.*` keys for setting the event metadata fields.
// If `@metadata` key is used then `ErrAlterMetadataKey` is returned.
func (e *Event) PutValue(key string, v interface{}) (interface{}, error) {
	if key == TimestampFieldKey {
		return e.setTimestamp(v)
	}
	if key == MetadataFieldKey {
		return nil, ErrAlterMetadataKey
	}

	if subKey, ok := e.metadataSubKey(key); ok {
		if e.Meta == nil {
			e.Meta = mapstr.M{}
		}

		return e.Meta.Put(subKey, v)
	}

	if e.fields == nil {
		e.fields = mapstr.M{}
	}

	if topKey, subKey, cm := e.cowField(key); cm != nil {
		if subKey == "" {
			// Replacing the entire sub-tree.
			e.fields[topKey] = v
			return cm.shared.Clone(), nil
		}
		// Copy-on-write: clone shared data, then mutate the clone.
		materialized := e.materializeCow(topKey, cm)
		return materialized.Put(subKey, v)
	}

	return e.fields.Put(key, v)
}

// PutValueQuiet sets a value without returning the old value.
// Use this in processors where the old value is not needed — it avoids
// cloning cowMap data that would otherwise be returned and discarded.
func (e *Event) PutValueQuiet(key string, v interface{}) error {
	if key == TimestampFieldKey {
		_, err := e.setTimestamp(v)
		return err
	}
	if key == MetadataFieldKey {
		return ErrAlterMetadataKey
	}

	if subKey, ok := e.metadataSubKey(key); ok {
		if e.Meta == nil {
			e.Meta = mapstr.M{}
		}
		_, err := e.Meta.Put(subKey, v)
		return err
	}

	if e.fields == nil {
		e.fields = mapstr.M{}
	}

	if topKey, subKey, cm := e.cowField(key); cm != nil {
		if subKey == "" {
			if _, isCow := v.(*cowMap); isCow {
				e.hasCow = true
			}
			e.fields[topKey] = v
			e.nFields++
			return nil
		}
		materialized := e.materializeCow(topKey, cm)
		_, err := materialized.Put(subKey, v)
		return err
	}

	if _, isCow := v.(*cowMap); isCow {
		e.hasCow = true
	}
	_, err := e.fields.Put(key, v)
	return err
}

// Delete deletes the given key from the event.
//
// Use `@metadata.*` keys for deleting the event metadata fields.
// If `@metadata` key is used then `ErrAlterMetadataKey` is returned.
// If `@timestamp` key is used then `ErrDeleteTimestamp` is returned.
func (e *Event) Delete(key string) error {
	if key == TimestampFieldKey {
		return ErrDeleteTimestamp
	}
	if key == MetadataFieldKey {
		return ErrAlterMetadataKey
	}
	if subKey, ok := e.metadataSubKey(key); ok {
		if e.Meta == nil {
			return mapstr.ErrKeyNotFound
		}
		return e.Meta.Delete(subKey)
	}

	if e.fields == nil {
		return mapstr.ErrKeyNotFound
	}

	if topKey, subKey, cm := e.cowField(key); cm != nil {
		if subKey == "" {
			// Deleting the entire sub-tree.
			delete(e.fields, topKey)
			return nil
		}
		// Copy-on-write: clone shared data, then delete from the clone.
		materialized := e.materializeCow(topKey, cm)
		return materialized.Delete(subKey)
	}

	return e.fields.Delete(key)
}

// CloneFields returns a deep copy of Fields with cowMap values properly
// handled. cowMap entries are shared by reference (both copies point to
// the same immutable data) — the COW mechanism handles isolation on write.
func (e *Event) CloneFields() mapstr.M {
	return e.cloneFields()
}

// cloneFields creates a copy of Fields. cowMap entries are shared
// by reference (both clones point to the same immutable data).
// Non-cowMap entries are deep-cloned normally.
func (e *Event) cloneFields() mapstr.M {
	if e.fields == nil {
		return nil
	}
	hasCow := false
	for _, v := range e.fields {
		if _, ok := v.(*cowMap); ok {
			hasCow = true
			break
		}
	}
	if !hasCow {
		return e.fields.Clone()
	}
	c := make(mapstr.M, len(e.fields))
	for k, v := range e.fields {
		if _, ok := v.(*cowMap); ok {
			c[k] = v // share cowMap reference
		} else if m, ok := v.(mapstr.M); ok {
			c[k] = m.Clone()
		} else {
			c[k] = v
		}
	}
	return c
}

func (e *Event) metadataSubKey(key string) (string, bool) {
	if !strings.HasPrefix(key, metadataKeyPrefix) {
		return "", false
	}

	subKey := key[metadataKeyOffset:]
	if subKey == "" {
		return "", false
	}
	return subKey, true
}

// SetErrorWithOption sets the event error field with the message when the addErrKey is set to true.
// If you want to include the data and field you can pass them as parameters and will be appended into the
// error as fields with the corresponding name.
func (e *Event) SetErrorWithOption(message string, addErrKey bool, data string, field string) {
	if !addErrKey {
		return
	}

	errorField := mapstr.M{"message": message, "type": "json"}
	if data != "" {
		errorField["data"] = data
	}
	if field != "" {
		errorField["field"] = field
	}
	e.fields[ErrorFieldKey] = errorField
}

// String returns a string representation of the event.
func (e *Event) String() string {
	m := mapstr.M{
		TimestampFieldKey: e.Timestamp,
		MetadataFieldKey:  mapstr.M{},
	}
	if e.Meta != nil {
		m[MetadataFieldKey] = e.Meta
	}
	// Unwrap cowMaps for display without modifying the event.
	fields := e.fields
	copied := false
	for k, v := range fields {
		if cm, ok := v.(*cowMap); ok {
			if !copied {
				fields = make(mapstr.M, len(e.fields))
				for k2, v2 := range e.fields {
					fields[k2] = v2
				}
				copied = true
			}
			fields[k] = cm.shared
		}
	}
	m.DeepUpdate(fields)
	return m.String()
}

// Flatten returns a flat representation of Fields with dot-separated keys.
// cowMap values are materialized before flattening.
func (e *Event) Flatten() mapstr.M {
	if e.fields == nil {
		return nil
	}
	e.Materialize()
	return e.fields.Flatten()
}

// FlattenKeys returns a flat list of all dot-separated key paths in Fields.
// cowMap values are materialized before flattening.
func (e *Event) FlattenKeys() *[]string {
	if e.fields == nil {
		return nil
	}
	e.Materialize()
	return e.fields.FlattenKeys()
}

// HasKey returns true if the key exist. If an error occurs then false is
// returned with a non-nil error.
func (e *Event) HasKey(key string) (bool, error) {
	if key == TimestampFieldKey || key == MetadataFieldKey {
		return true, nil
	}

	if subKey, ok := e.metadataSubKey(key); ok {
		if e.Meta == nil {
			return false, nil
		}
		return e.Meta.HasKey(subKey)
	}

	if e.fields == nil {
		return false, nil
	}

	if _, subKey, cm := e.cowField(key); cm != nil {
		if subKey == "" {
			return true, nil
		}
		return cm.shared.HasKey(subKey)
	}

	return e.fields.HasKey(key)
}
