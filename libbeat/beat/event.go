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
	return e.CloneFields()
}

// SetFields sets the event's fields from a mapstr.M.
func (e *Event) SetFields(m mapstr.M) {
	if e.fields == nil {
		e.fields = make(mapstr.M, defaultFieldsCap)
	} else {
		clear(e.fields)
	}
	e.fieldCount = 0
	e.hasCow = false
	for k, v := range m {
		e.fields[k] = v
		e.fieldCount++
		if _, ok := v.(*cowMap); ok {
			e.hasCow = true
		}
	}
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

	topKey, subKey := splitDot(key)

	val, ok := e.fields[topKey]
	if !ok {
		return nil, mapstr.ErrKeyNotFound
	}

	if subKey == "" {
		switch v := val.(type) {
		case *cowMap:
			return v.shared.Clone(), nil
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
	case mapstr.M:
		return v.GetValue(subKey)
	default:
		return nil, mapstr.ErrKeyNotFound
	}
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

	e.ensureFields()
	topKey, subKey := splitDot(key)

	if subKey == "" {
		if _, exists := e.fields[topKey]; !exists {
			e.fieldCount++
		}
		e.fields[topKey] = v
		if _, ok := v.(*cowMap); ok {
			e.hasCow = true
		}
		return nil
	}

	_, err := e.putDotted(topKey, subKey, v)
	return err
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

	val, ok := e.fields[topKey]
	if !ok {
		return mapstr.ErrKeyNotFound
	}

	switch ev := val.(type) {
	case *cowMap:
		cloned := ev.shared.Clone()
		e.fields[topKey] = cloned
		return cloned.Delete(subKey)
	case mapstr.M:
		return ev.Delete(subKey)
	default:
		return mapstr.ErrKeyNotFound
	}
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

	val, ok := e.fields[topKey]
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
				// Check if clone is needed: skip if all source keys exist.
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
			cloned := ev.shared.Clone()
			switch mode {
			case updateModeOverwrite:
				cloned.DeepUpdate(srcMap)
			case updateModeNoOverwrite:
				cloned.DeepUpdateNoOverwrite(srcMap)
			}
			e.fields[k] = cloned
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
		newMap := mapstr.M{}
		old, err := newMap.Put(subKey, v)
		if err != nil {
			return nil, err
		}
		e.fields[topKey] = newMap
		e.fieldCount++
		return old, nil
	}

	switch ev := existing.(type) {
	case *cowMap:
		cloned := ev.shared.Clone()
		e.fields[topKey] = cloned
		return cloned.Put(subKey, v)
	case mapstr.M:
		return ev.Put(subKey, v)
	default:
		newMap := mapstr.M{}
		old, err := newMap.Put(subKey, v)
		if err != nil {
			return nil, err
		}
		e.fields[topKey] = newMap
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
	e.ensureFields()
	e.fields[ErrorFieldKey] = errorField
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

func splitDot(key string) (topKey, subKey string) {
	if dot := strings.IndexByte(key, '.'); dot >= 0 {
		return key[:dot], key[dot+1:]
	}
	return key, ""
}
