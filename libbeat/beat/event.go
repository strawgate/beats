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

const FlagField = "log.flags"

const (
	TimestampFieldKey = "@timestamp"
	MetadataFieldKey  = "@metadata"
	ErrorFieldKey     = "error"
	metadataKeyPrefix = MetadataFieldKey + "."
	metadataKeyOffset = len(metadataKeyPrefix)

	defaultFieldsCap    = 12
	maxPooledFieldCount = 16
)

// Event is the common event format shared by all beats.
type Event struct {
	Timestamp  time.Time
	Meta       mapstr.M
	Private    interface{}
	TimeSeries bool

	fields     mapstr.M // private — use Fields()/SetFields()/GetValue/PutValue
	fieldCount int      // tracks top-level key insertions for pool sizing
	hasCow     bool     // true when fields contains at least one cowMap value
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
	New: func() interface{} {
		return &Event{
			fields: make(mapstr.M, defaultFieldsCap),
		}
	},
}

func NewEvent() *Event {
	return eventPool.Get().(*Event) //nolint:errcheck
}

func ReleaseEvent(e *Event) {
	if e.fieldCount > maxPooledFieldCount {
		e.fields = make(mapstr.M, defaultFieldsCap)
	} else {
		clear(e.fields)
	}
	e.fieldCount = 0
	e.hasCow = false
	e.Timestamp = time.Time{}
	e.Meta = nil
	e.Private = nil
	e.TimeSeries = false
	eventPool.Put(e)
}

// --- Fields access ---

// Fields returns a deep copy of the event's fields as a mapstr.M.
// cowMap values are unwrapped and cloned. Safe for mutation by callers.
func (e *Event) Fields() mapstr.M {
	if e.fields == nil {
		return nil
	}
	if !e.hasCow {
		return e.fields.Clone()
	}
	c := make(mapstr.M, len(e.fields))
	for k, v := range e.fields {
		switch val := v.(type) {
		case *cowMap:
			c[k] = val.shared.Clone()
		case microMap:
			c[k] = val.ToMapStr()
		case mapstr.M:
			c[k] = val.Clone()
		default:
			c[k] = v
		}
	}
	return c
}

// SetFields sets the event's fields from a mapstr.M.
// Takes ownership of the map — caller must not modify it after this call.
func (e *Event) SetFields(m mapstr.M) {
	e.fields = m
	e.fieldCount = len(m)
	e.hasCow = false
}

// CloneFields returns a deep copy of fields. cowMap entries are shared
// by reference (COW handles isolation on write).
func (e *Event) CloneFields() mapstr.M {
	if e.fields == nil {
		return nil
	}
	if !e.hasCow {
		return e.fields.Clone()
	}
	c := make(mapstr.M, len(e.fields))
	for k, v := range e.fields {
		switch val := v.(type) {
		case *cowMap:
			c[k] = val // share cowMap reference
		case microMap:
			c[k] = val.Clone()
		case mapstr.M:
			c[k] = val.Clone()
		default:
			c[k] = v
		}
	}
	return c
}

// FieldsLen returns the number of top-level fields without allocating.
func (e *Event) FieldsLen() int {
	return len(e.fields)
}

// FieldsUnsafe returns the internal fields map directly. Values may be
// *cowMap or microMap types. Only safe for consumers that understand
// these types (e.g., the encoder with registered Folders).
// Callers MUST NOT modify the returned map or any nested values.
func (e *Event) FieldsUnsafe() mapstr.M {
	return e.fields
}

// Materialize is an alias for Fields().
func (e *Event) Materialize() mapstr.M {
	return e.Fields()
}

// --- Core accessors ---

func (e *Event) SetID(id string) {
	_, _ = e.PutValue(metadataKeyPrefix+"_id", id)
}

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

	// Fast path: top-level key, no dots.
	if strings.IndexByte(key, '.') < 0 {
		val, ok := e.fields[key]
		if !ok {
			return nil, mapstr.ErrKeyNotFound
		}
		switch v := val.(type) {
		case *cowMap:
			return v.shared.Clone(), nil
		case microMap:
			return v.ToMapStr(), nil
		default:
			return v, nil
		}
	}

	topKey, subKey := splitDot(key)
	if val, ok := e.fields[topKey]; ok {
		if subKey == "" {
			switch v := val.(type) {
			case *cowMap:
				return v.shared.Clone(), nil
			case microMap:
				return v.ToMapStr(), nil
			default:
				return v, nil
			}
		}
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
		case microMap:
			return microMapGetDotted(v, subKey)
		}
	}

	// Delegate to mapstr for dotted-key navigation and literal key fallback.
	return e.fields.GetValue(key)
}

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

	e.ensureFields()
	topKey, subKey := splitDot(key)

	if subKey == "" {
		old := e.fields[topKey]
		if old == nil {
			e.fieldCount++
		}
		e.fields[topKey] = v
		if _, ok := v.(*cowMap); ok {
			e.hasCow = true
		}
		if cm, ok := old.(*cowMap); ok {
			return cm.shared.Clone(), nil
		}
		return old, nil
	}

	return e.putDotted(topKey, subKey, v)
}

// SetField sets a top-level key directly without dot splitting.
// Matches the old event.Fields[key] = value behavior.
// Does not handle @timestamp or @metadata — use PutValue for those.
func (e *Event) SetField(key string, v interface{}) {
	e.ensureFields()
	if _, exists := e.fields[key]; !exists {
		e.fieldCount++
	}
	e.fields[key] = v
	if _, ok := v.(*cowMap); ok {
		e.hasCow = true
	}
}

// PutValueQuiet is an alias for SetField for backward compatibility.
func (e *Event) PutValueQuiet(key string, v interface{}) error {
	e.SetField(key, v)
	return nil
}

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

	topKey, subKey := splitDot(key)

	if subKey == "" {
		if _, ok := e.fields[topKey]; ok {
			delete(e.fields, topKey)
			return nil
		}
		return mapstr.ErrKeyNotFound
	}

	if val, ok := e.fields[topKey]; ok {
		switch v := val.(type) {
		case *cowMap:
			mm := microMapFromMapStr(v.shared)
			updated, err := microMapDeleteDotted(mm, subKey)
			if err != nil {
				return err
			}
			e.fields[topKey] = updated
			return nil
		case microMap:
			updated, err := microMapDeleteDotted(v, subKey)
			if err != nil {
				return err
			}
			e.fields[topKey] = updated
			return nil
		}
	}

	// Delegate to mapstr for dotted-key navigation and literal key fallback.
	return e.fields.Delete(key)
}

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

	topKey, subKey := splitDot(key)

	if val, ok := e.fields[topKey]; ok {
		if subKey == "" {
			return true, nil
		}
		switch v := val.(type) {
		case *cowMap:
			return v.shared.HasKey(subKey)
		case microMap:
			_, err := microMapGetDotted(v, subKey)
			return err == nil, nil
		}
	}

	// Delegate to mapstr for dotted-key navigation and literal key fallback.
	return e.fields.HasKey(key)
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

	timestampValue, timestampExists := d[TimestampFieldKey]
	if timestampExists {
		if mode == updateModeOverwrite {
			_, _ = e.setTimestamp(timestampValue)
		}
		delete(d, TimestampFieldKey)
		defer func() { d[TimestampFieldKey] = timestampValue }()
	}

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

	e.ensureFields()

	for k, v := range d {
		existing, exists := e.fields[k]

		var srcMap mapstr.M
		switch sv := v.(type) {
		case mapstr.M:
			srcMap = sv
		case map[string]interface{}:
			srcMap = mapstr.M(sv)
		}

		if srcMap == nil || !exists {
			if mode == updateModeOverwrite || !exists {
				if !exists {
					e.fieldCount++
				}
				e.fields[k] = v
			}
			continue
		}

		switch ev := existing.(type) {
		case *cowMap:
			if mode == updateModeNoOverwrite {
				allExist := true
				for sk := range srcMap {
					if _, ok := ev.shared[sk]; !ok {
						allExist = false
						break
					}
				}
				if allExist {
					continue
				}
			}
			// Clone as microMap instead of mapstr.M.
			mm := microMapFromMapStr(ev.shared)
			for sk, sv := range srcMap {
				existing, found := mm.Get(sk)
				if found && mode == updateModeNoOverwrite {
					continue
				}
				_ = existing
				mm = mm.Set(sk, convertNestedValue(sv))
			}
			e.fields[k] = mm
		case microMap:
			for sk, sv := range srcMap {
				_, found := ev.Get(sk)
				if found && mode == updateModeNoOverwrite {
					continue
				}
				ev = ev.Set(sk, convertNestedValue(sv))
			}
			e.fields[k] = ev
		case mapstr.M:
			switch mode {
			case updateModeOverwrite:
				ev.DeepUpdate(srcMap)
			case updateModeNoOverwrite:
				ev.DeepUpdateNoOverwrite(srcMap)
			}
		default:
			if mode == updateModeOverwrite {
				e.fields[k] = v
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
		hasCow:     e.hasCow,
	}
	c.fields = e.CloneFields()
	return c
}

// --- Utility ---

func (e *Event) ensureFields() {
	if e.fields == nil {
		e.fields = make(mapstr.M, defaultFieldsCap)
	}
}

func (e *Event) putDotted(topKey, subKey string, v interface{}) (interface{}, error) {
	existing, ok := e.fields[topKey]
	if !ok {
		// Create new nested structure as microMap.
		newMap := mapstr.M{}
		old, err := newMap.Put(subKey, v)
		if err != nil {
			return nil, err
		}
		e.fields[topKey] = microMapFromMapStr(newMap)
		e.fieldCount++
		return old, nil
	}

	switch ev := existing.(type) {
	case *cowMap:
		// Clone cowMap as microMap instead of mapstr.M.
		mm := microMapFromMapStr(ev.shared)
		updated := microMapSetDotted(mm, subKey, v)
		e.fields[topKey] = updated
		return nil, nil // old value not available from microMap path
	case microMap:
		updated := microMapSetDotted(ev, subKey, v)
		e.fields[topKey] = updated
		return nil, nil
	case mapstr.M:
		return ev.Put(subKey, v)
	default:
		newMap := mapstr.M{}
		old, err := newMap.Put(subKey, v)
		if err != nil {
			return nil, err
		}
		e.fields[topKey] = microMapFromMapStr(newMap)
		return old, nil
	}
}

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
	_ = e.PutValueQuiet(ErrorFieldKey, errorField)
}

func (e *Event) String() string {
	m := mapstr.M{
		TimestampFieldKey: e.Timestamp,
		MetadataFieldKey:  mapstr.M{},
	}
	if e.Meta != nil {
		m[MetadataFieldKey] = e.Meta
	}
	m.DeepUpdate(e.Fields())
	return m.String()
}

func (e *Event) Flatten() mapstr.M {
	f := e.Fields()
	if f == nil {
		return nil
	}
	return f.Flatten()
}

func (e *Event) FlattenKeys() *[]string {
	f := e.Fields()
	if f == nil {
		return nil
	}
	return f.FlattenKeys()
}

// microMapGetDotted navigates a dotted key through nested microMaps.
func microMapGetDotted(mm microMap, key string) (interface{}, error) {
	topKey, subKey := splitDot(key)
	val, ok := mm.Get(topKey)
	if !ok {
		return nil, mapstr.ErrKeyNotFound
	}
	if subKey == "" {
		if nested, ok := val.(microMap); ok {
			return nested.ToMapStr(), nil
		}
		return val, nil
	}
	switch v := val.(type) {
	case microMap:
		return microMapGetDotted(v, subKey)
	case mapstr.M:
		return v.GetValue(subKey)
	default:
		return nil, mapstr.ErrKeyNotFound
	}
}

// microMapSetDotted navigates a dotted key through nested microMaps,
// creating structure as needed. Returns the updated root microMap.
func microMapSetDotted(mm microMap, key string, value interface{}) microMap {
	topKey, subKey := splitDot(key)
	if subKey == "" {
		return mm.Set(topKey, value)
	}
	existing, ok := mm.Get(topKey)
	if !ok {
		// Create nested microMap.
		nested := microMap(&mm1{k: subKey, v: value})
		// But subKey might itself have dots — handle recursively.
		if subTop, subSub := splitDot(subKey); subSub != "" {
			nested = microMapSetDotted(&mm1{k: subTop, v: nil}, subSub, value)
			nested = nested.Delete(subTop)
			nested = nested.Set(subTop, microMapSetDotted(&mm1{}, subKey, value))
			// Simpler: just use a single mm1 and let it grow.
		}
		// Actually, simplest approach: create via mapstr.M.Put then convert.
		newMap := mapstr.M{}
		_, _ = newMap.Put(subKey, value)
		return mm.Set(topKey, microMapFromMapStr(newMap))
	}
	switch v := existing.(type) {
	case microMap:
		updated := microMapSetDotted(v, subKey, value)
		return mm.Set(topKey, updated)
	case mapstr.M:
		_, _ = v.Put(subKey, value)
		return mm
	default:
		newMap := mapstr.M{}
		_, _ = newMap.Put(subKey, value)
		return mm.Set(topKey, microMapFromMapStr(newMap))
	}
}

// microMapDeleteDotted deletes a dotted key from a microMap.
func microMapDeleteDotted(mm microMap, key string) (microMap, error) {
	topKey, subKey := splitDot(key)
	if subKey == "" {
		_, found := mm.Get(topKey)
		if !found {
			return mm, mapstr.ErrKeyNotFound
		}
		return mm.Delete(topKey), nil
	}
	val, found := mm.Get(topKey)
	if !found {
		return mm, mapstr.ErrKeyNotFound
	}
	switch v := val.(type) {
	case microMap:
		updated, err := microMapDeleteDotted(v, subKey)
		if err != nil {
			return mm, err
		}
		return mm.Set(topKey, updated), nil
	case mapstr.M:
		err := v.Delete(subKey)
		return mm, err
	default:
		return mm, mapstr.ErrKeyNotFound
	}
}

func splitDot(key string) (topKey, subKey string) {
	if dot := strings.IndexByte(key, '.'); dot >= 0 {
		return key[:dot], key[dot+1:]
	}
	return key, ""
}
