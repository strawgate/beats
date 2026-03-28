package beat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/mapstr"
)

func TestMicroMapBasic(t *testing.T) {
	mm := microMapFromMapStr(mapstr.M{"key": "value"})
	require.NotNil(t, mm)
	assert.Equal(t, 1, mm.Len())

	v, ok := mm.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", v)
}

func TestMicroMapNested(t *testing.T) {
	m := mapstr.M{
		"provider": "aws",
		"instance": mapstr.M{"id": "i-abc"},
	}
	mm := microMapFromMapStr(m)

	v, ok := mm.Get("provider")
	require.True(t, ok)
	assert.Equal(t, "aws", v)

	v, ok = mm.Get("instance")
	require.True(t, ok)
	nested, ok := v.(microMap)
	require.True(t, ok)
	id, ok := nested.Get("id")
	require.True(t, ok)
	assert.Equal(t, "i-abc", id)
}

func TestMicroMapSetPromotes(t *testing.T) {
	mm := microMap(&mm1{k: "a", v: 1})
	mm = mm.Set("b", 2)
	assert.Equal(t, 2, mm.Len())

	v, _ := mm.Get("a")
	assert.Equal(t, 1, v)
	v, _ = mm.Get("b")
	assert.Equal(t, 2, v)
}

func TestMicroMapDeleteDemotes(t *testing.T) {
	mm := microMap(&mm2{k1: "a", k2: "b", v1: 1, v2: 2})
	mm = mm.Delete("a")
	assert.Equal(t, 1, mm.Len())

	v, ok := mm.Get("b")
	assert.True(t, ok)
	assert.Equal(t, 2, v)
}

func TestMicroMapToMapStr(t *testing.T) {
	m := mapstr.M{
		"provider": "aws",
		"instance": mapstr.M{"id": "i-abc"},
	}
	mm := microMapFromMapStr(m)
	result := mm.ToMapStr()

	assert.Equal(t, "aws", result["provider"])
	inst, ok := result["instance"].(mapstr.M)
	require.True(t, ok)
	assert.Equal(t, "i-abc", inst["id"])
}

func TestMicroMapClone(t *testing.T) {
	mm := microMapFromMapStr(mapstr.M{"key": "value"})
	c := mm.Clone()

	c = c.Set("key", "modified")
	v, _ := mm.Get("key")
	assert.Equal(t, "value", v) // original unchanged
}

func TestMicroMapPromoteToMmN(t *testing.T) {
	var mm microMap = &mm1{k: "k1", v: "v1"}
	for i := 2; i <= 10; i++ {
		mm = mm.Set(
			"k"+string(rune('0'+i)),
			"v"+string(rune('0'+i)),
		)
	}
	assert.Equal(t, 10, mm.Len())

	// Add 11th — should promote to mmMapWrap
	mm = mm.Set("k11", "v11")
	assert.Equal(t, 11, mm.Len())
	_, isMap := mm.(*mmMapWrap)
	assert.True(t, isMap)
}

// --- Benchmarks ---

var microMapSink interface{}

var cloudData = mapstr.M{
	"provider":          "aws",
	"region":            "us-east-1",
	"availability_zone": "us-east-1a",
	"account":           mapstr.M{"id": "123456789012"},
	"instance":          mapstr.M{"id": "i-0abcdef"},
	"machine":           mapstr.M{"type": "m5.xlarge"},
	"service":           mapstr.M{"name": "EC2"},
}

func BenchmarkCloneMapStr(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		microMapSink = cloudData.Clone()
	}
}

func BenchmarkCloneMicroMap(b *testing.B) {
	mm := microMapFromMapStr(cloudData)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		microMapSink = mm.Clone()
	}
}

func BenchmarkToMapStrFromMicroMap(b *testing.B) {
	mm := microMapFromMapStr(cloudData)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		microMapSink = mm.ToMapStr()
	}
}
