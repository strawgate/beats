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
type Event struct {
	Timestamp time.Time
	Meta      mapstr.M

	// fields is the primary storage for event data. Uses SmallMap for
	// zero-allocation storage of typical events (≤20 top-level keys).
	fields SmallMap

	// Private is for input-specific data.
	Private    interface{}
	TimeSeries bool
}

var (
	ErrValueNotTimestamp = errors.New("value is not a timestamp")
	ErrValueNotMapStr    = errors.New("value is not `mapstr.M` or `map[string]interface{}` type")
	ErrAlterMetadataKey  = fmt.Errorf("deleting/replacing %q key is not supported", MetadataFieldKey)
	ErrMetadataAccess    = fmt.Errorf("accessing %q key directly is not supported, try nested keys", MetadataFieldKey)
	ErrDeleteTimestamp   = fmt.Errorf("deleting %q key is not supported", TimestampFieldKey)
)

// --- Pool ---

var eventPool = sync.Pool{
	New: func() interface{} { return &Event{} },
}

// NewEvent returns an Event from the pool.
func NewEvent() *Event {
	return eventPool.Get().(*Event) //nolint:errcheck
}

// ReleaseEvent returns an Event to the pool for reuse.
func ReleaseEvent(e *Event) {
	e.fields.Clear()
	e.Timestamp = time.Time{}
	e.Meta = nil
	e.Private = nil
	e.TimeSeries = false
	eventPool.Put(e)
}

// --- Fields access ---

// Fields returns the event's fields as a mapstr.M. cowMap values are
// unwrapped to their shared references. The returned map is suitable
// for read-only use (encoding). For a mutable copy, use CloneFields.
func (e *Event) Fields() mapstr.M {
	return e.renderFields()
}

// SetFields sets the event's fields from a mapstr.M.
func (e *Event) SetFields(m mapstr.M) {
	e.fields.Clear()
	for k, v := range m {
		e.fields.Set(k, v)
	}
}

// CloneFields returns a deep copy of the fields. cowMap entries are
// shared by reference (COW handles isolation on write).
func (e *Event) CloneFields() mapstr.M {
	m := make(mapstr.M, e.fields.Len())
	e.fields.Range(func(k string, v interface{}) bool {
		if _, ok := v.(*cowMap); ok {
			m[k] = v // share cowMap reference
		} else if sub, ok := v.(mapstr.M); ok {
			m[k] = sub.Clone()
		} else {
			m[k] = v
		}
		return true
	})
	return m
}

// renderFields builds a mapstr.M from the SmallMap, unwrapping cowMaps.
func (e *Event) renderFields() mapstr.M {
	if e.fields.Len() == 0 {
		return nil
	}
	m := make(mapstr.M, e.fields.Len())
	e.fields.Range(func(k string, v interface{}) bool {
		if cm, ok := v.(*cowMap); ok {
			m[k] = cm.shared
		} else {
			m[k] = v
		}
		return true
	})
	return m
}

// --- Core accessors ---

// SetID overwrites the "id" field in the events metadata.
func (e *Event) SetID(id string) {
	_, _ = e.PutValue(metadataKeyPrefix+"_id", id)
}

// GetValue gets a value from the event.
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

	topKey, subKey := splitDot(key)
	val, ok := e.fields.Get(topKey)
	if !ok {
		return nil, mapstr.ErrKeyNotFound
	}

	if subKey == "" {
		// Top-level read — clone cowMap/map sub-trees for safety.
		switch v := val.(type) {
		case *cowMap:
			return v.shared.Clone(), nil
		default:
			return v, nil
		}
	}

	// Dotted key — navigate into the value.
	switch v := val.(type) {
	case *cowMap:
		result, err := v.shared.GetValue(subKey)
		if err != nil {
			return nil, err
		}
		if m, ok := result.(mapstr.M); ok {
			return m.Clone(), nil
		}
		return result, nil
	case mapstr.M:
		return v.GetValue(subKey)
	default:
		return nil, mapstr.ErrKeyNotFound
	}
}

// PutValue sets a value, returning the old value.
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

	topKey, subKey := splitDot(key)

	if subKey == "" {
		old, _ := e.fields.Get(topKey)
		e.fields.Set(topKey, v)
		// Return clone of old cowMap to honor contract.
		if cm, ok := old.(*cowMap); ok {
			return cm.shared.Clone(), nil
		}
		return old, nil
	}

	// Dotted key — navigate into sub-map.
	existing, ok := e.fields.Get(topKey)
	if !ok {
		// Create new nested map.
		newMap := mapstr.M{}
		old, err := newMap.Put(subKey, v)
		if err != nil {
			return nil, err
		}
		e.fields.Set(topKey, newMap)
		return old, nil
	}

	switch ev := existing.(type) {
	case *cowMap:
		// Copy-on-write.
		cloned := ev.shared.Clone()
		e.fields.Set(topKey, cloned)
		return cloned.Put(subKey, v)
	case mapstr.M:
		return ev.Put(subKey, v)
	default:
		newMap := mapstr.M{}
		old, err := newMap.Put(subKey, v)
		if err != nil {
			return nil, err
		}
		e.fields.Set(topKey, newMap)
		return old, nil
	}
}

// PutValueQuiet sets a value without returning the old value.
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

	topKey, subKey := splitDot(key)

	if subKey == "" {
		e.fields.Set(topKey, v)
		return nil
	}

	// Dotted key — navigate into sub-map.
	existing, ok := e.fields.Get(topKey)
	if !ok {
		newMap := mapstr.M{}
		_, err := newMap.Put(subKey, v)
		if err != nil {
			return err
		}
		e.fields.Set(topKey, newMap)
		return nil
	}

	switch ev := existing.(type) {
	case *cowMap:
		cloned := ev.shared.Clone()
		e.fields.Set(topKey, cloned)
		_, err := cloned.Put(subKey, v)
		return err
	case mapstr.M:
		_, err := ev.Put(subKey, v)
		return err
	default:
		newMap := mapstr.M{}
		_, err := newMap.Put(subKey, v)
		if err != nil {
			return err
		}
		e.fields.Set(topKey, newMap)
		return nil
	}
}

// Delete deletes the given key from the event.
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

	topKey, subKey := splitDot(key)

	if subKey == "" {
		if e.fields.Delete(topKey) {
			return nil
		}
		return mapstr.ErrKeyNotFound
	}

	existing, ok := e.fields.Get(topKey)
	if !ok {
		return mapstr.ErrKeyNotFound
	}

	switch ev := existing.(type) {
	case *cowMap:
		cloned := ev.shared.Clone()
		e.fields.Set(topKey, cloned)
		return cloned.Delete(subKey)
	case mapstr.M:
		return ev.Delete(subKey)
	default:
		return mapstr.ErrKeyNotFound
	}
}

// HasKey returns true if the key exists.
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

	topKey, subKey := splitDot(key)

	val, ok := e.fields.Get(topKey)
	if !ok {
		return false, nil
	}
	if subKey == "" {
		return true, nil
	}

	switch v := val.(type) {
	case *cowMap:
		return v.shared.HasKey(subKey)
	case mapstr.M:
		return v.HasKey(subKey)
	default:
		return false, nil
	}
}

// --- DeepUpdate ---

func (e *Event) DeepUpdate(d mapstr.M) {
	e.deepUpdate(d, updateModeOverwrite)
}

func (e *Event) DeepUpdateNoOverwrite(d mapstr.M) {
	e.deepUpdate(d, updateModeNoOverwrite)
}

func (e *Event) deepUpdate(d mapstr.M, mode updateMode) {
	if len(d) == 0 {
		return
	}

	// Handle @timestamp.
	timestampValue, timestampExists := d[TimestampFieldKey]
	if timestampExists {
		if mode == updateModeOverwrite {
			_, _ = e.setTimestamp(timestampValue)
		}
		delete(d, TimestampFieldKey)
		defer func() { d[TimestampFieldKey] = timestampValue }()
	}

	// Handle @metadata.
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
		delete(d, MetadataFieldKey)
		defer func() { d[MetadataFieldKey] = metaValue }()
	}

	if len(d) == 0 {
		return
	}

	// Merge remaining keys into fields.
	for k, v := range d {
		existing, exists := e.fields.Get(k)

		// Check if update value is a map that needs merging.
		var srcMap mapstr.M
		switch sv := v.(type) {
		case mapstr.M:
			srcMap = sv
		case map[string]interface{}:
			srcMap = mapstr.M(sv)
		}

		if srcMap == nil || !exists {
			// Scalar or new key — just set.
			if mode == updateModeOverwrite || !exists {
				e.fields.Set(k, v)
			}
			continue
		}

		// Both sides are maps — merge.
		switch ev := existing.(type) {
		case *cowMap:
			cloned := ev.shared.Clone()
			switch mode {
			case updateModeOverwrite:
				cloned.DeepUpdate(srcMap)
			case updateModeNoOverwrite:
				cloned.DeepUpdateNoOverwrite(srcMap)
			}
			e.fields.Set(k, cloned)
		case mapstr.M:
			switch mode {
			case updateModeOverwrite:
				ev.DeepUpdate(srcMap)
			case updateModeNoOverwrite:
				ev.DeepUpdateNoOverwrite(srcMap)
			}
		default:
			if mode == updateModeOverwrite {
				e.fields.Set(k, v)
			}
		}
	}
}

// --- Clone ---

func (e *Event) Clone() *Event {
	c := &Event{
		Timestamp:  e.Timestamp,
		Meta:       e.Meta.Clone(),
		Private:    e.Private,
		TimeSeries: e.TimeSeries,
	}
	// Clone SmallMap — cowMap entries are shared by reference.
	c.fields = e.fields.Clone()
	return c
}

// --- Utility ---

func (e *Event) setTimestamp(v interface{}) (interface{}, error) {
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

// SetErrorWithOption sets the event error field.
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
	e.fields.Set(ErrorFieldKey, errorField)
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
	m.DeepUpdate(e.renderFields())
	return m.String()
}

// Flatten returns a flat map with dot-separated keys.
func (e *Event) Flatten() mapstr.M {
	f := e.renderFields()
	if f == nil {
		return nil
	}
	return f.Flatten()
}

// FlattenKeys returns a flat list of all dot-separated key paths.
func (e *Event) FlattenKeys() *[]string {
	f := e.renderFields()
	if f == nil {
		return nil
	}
	return f.FlattenKeys()
}

// Materialize is an alias for Fields() — renders the SmallMap into a
// mapstr.M with cowMaps unwrapped. Kept for backward compatibility.
func (e *Event) Materialize() mapstr.M {
	return e.renderFields()
}

// splitDot splits a key on the first dot. If no dot, subKey is empty.
func splitDot(key string) (topKey, subKey string) {
	if dot := strings.IndexByte(key, '.'); dot >= 0 {
		return key[:dot], key[dot+1:]
	}
	return key, ""
}
