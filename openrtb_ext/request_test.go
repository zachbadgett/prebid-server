package openrtb_ext

import (
	"reflect"
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/stretchr/testify/assert"
)

// Test the unmashalling of the prebid extensions and setting default Price Granularity
func TestExtRequestTargeting(t *testing.T) {
	extRequest := &ExtRequest{}
	err := jsoniter.Unmarshal([]byte(ext1), extRequest)
	if err != nil {
		t.Errorf("ext1 Unmashall falure: %s", err.Error())
	}
	if extRequest.Prebid.Targeting != nil {
		t.Error("ext1 Targeting is not nil")
	}

	extRequest = &ExtRequest{}
	err = jsoniter.Unmarshal([]byte(ext2), extRequest)
	if err != nil {
		t.Errorf("ext2 Unmashall falure: %s", err.Error())
	}
	if extRequest.Prebid.Targeting == nil {
		t.Error("ext2 Targeting is nil")
	} else {
		pgDense := PriceGranularityFromString("dense")
		if !reflect.DeepEqual(extRequest.Prebid.Targeting.PriceGranularity, pgDense) {
			t.Errorf("ext2 expected Price granularity \"dense\" (%v), found \"%v\"", pgDense, extRequest.Prebid.Targeting.PriceGranularity)
		}
	}

	extRequest = &ExtRequest{}
	err = jsoniter.Unmarshal([]byte(ext3), extRequest)
	if err != nil {
		t.Errorf("ext3 Unmashall falure: %s", err.Error())
	}
	if extRequest.Prebid.Targeting == nil {
		t.Error("ext3 Targeting is nil")
	} else {
		pgMed := PriceGranularityFromString("medium")
		if !reflect.DeepEqual(extRequest.Prebid.Targeting.PriceGranularity, pgMed) {
			t.Errorf("ext3 expected Price granularity \"medium\", found \"%v\"", extRequest.Prebid.Targeting.PriceGranularity)
		}
	}
}

const ext1 = `{
	"prebid": {
		"non_target": "some junk"
	}
}
`

const ext2 = `{
	"prebid": {
		"targeting": {
			"pricegranularity": "dense"
		}
	}
}`

const ext3 = `{
	"prebid": {
		"targeting": { }
	}
}`

func TestCacheIllegal(t *testing.T) {
	var bids ExtRequestPrebidCache
	if err := jsoniter.Unmarshal([]byte(`{}`), &bids); err == nil {
		t.Error("Unmarshal should fail when cache.bids is undefined.")
	}
	if err := jsoniter.Unmarshal([]byte(`{"bids":null}`), &bids); err == nil {
		t.Error("Unmarshal should fail when cache.bids is null.")
	}
	if err := jsoniter.Unmarshal([]byte(`{"bids":true}`), &bids); err == nil {
		t.Error("Unmarshal should fail when cache.bids is not an object.")
	}
}

func TestCacheBids(t *testing.T) {
	var bids ExtRequestPrebidCache
	assert.NoError(t, jsoniter.Unmarshal([]byte(`{"bids":{}}`), &bids))
	assert.NotNil(t, bids.Bids)
	assert.Nil(t, bids.VastXML)
}

func TestCacheVast(t *testing.T) {
	var bids ExtRequestPrebidCache
	assert.NoError(t, jsoniter.Unmarshal([]byte(`{"vastxml":{}}`), &bids))
	assert.Nil(t, bids.Bids)
	assert.NotNil(t, bids.VastXML)
}

func TestCacheNothing(t *testing.T) {
	var bids ExtRequestPrebidCache
	assert.Error(t, jsoniter.Unmarshal([]byte(`{}`), &bids))
}

type granularityTestData struct {
	json   []byte
	target PriceGranularity
}

func TestGranularityUnmarshal(t *testing.T) {
	for _, test := range validGranularityTests {
		var resolved PriceGranularity
		err := jsoniter.Unmarshal(test.json, &resolved)
		if err != nil {
			t.Errorf("Failed to Unmarshall granularity: %s", err.Error())
		}
		if !reflect.DeepEqual(test.target, resolved) {
			t.Errorf("Granularity unmarshal failed, the unmarshalled JSON did not match the target\nExpected: %v\nActual  : %v", test.target, resolved)
		}
	}
}

var validGranularityTests []granularityTestData = []granularityTestData{
	{
		json: []byte(`{"precision": 4, "ranges": [{"min": 0, "max": 5, "increment": 0.1}, {"min": 5, "max":10, "increment":0.5}, {"min":10, "max":20, "increment":1}]}`),
		target: PriceGranularity{
			Precision: 4,
			Ranges: []GranularityRange{
				{Min: 0.0, Max: 5.0, Increment: 0.1},
				{Min: 5.0, Max: 10.0, Increment: 0.5},
				{Min: 10.0, Max: 20.0, Increment: 1.0},
			},
		},
	},
	{
		json: []byte(`{"ranges":[{ "max":5, "increment": 0.05}, {"max": 10, "increment": 0.25}, {"max": 20, "increment": 0.5}]}`),
		target: PriceGranularity{
			Precision: 2,
			Ranges: []GranularityRange{
				{Min: 0.0, Max: 5.0, Increment: 0.05},
				{Min: 5.0, Max: 10.0, Increment: 0.25},
				{Min: 10.0, Max: 20.0, Increment: 0.5},
			},
		},
	},
	{
		json:   []byte(`"medium"`),
		target: priceGranularityMed,
	},
	{
		json: []byte(`{ "precision": 3, "ranges": [{"max":20, "increment":0.005}]}`),
		target: PriceGranularity{
			Precision: 3,
			Ranges:    []GranularityRange{{Min: 0.0, Max: 20.0, Increment: 0.005}},
		},
	},
	{
		json: []byte(`{"precision": 0, "ranges": [{"max":5, "increment": 1}, {"max": 10, "increment": 2}, {"max": 20, "increment": 5}]}`),
		target: PriceGranularity{
			Precision: 0,
			Ranges: []GranularityRange{
				{Min: 0.0, Max: 5.0, Increment: 1.0},
				{Min: 5.0, Max: 10.0, Increment: 2.0},
				{Min: 10.0, Max: 20.0, Increment: 5.0},
			},
		},
	},
	{
		json: []byte(`{"precision": 2, "ranges": [{"min": 0.5, "max":5, "increment": 0.1}, {"min": 54, "max": 10, "increment": 1}, {"min": -42, "max": 20, "increment": 5}]}`),
		target: PriceGranularity{
			Precision: 2,
			Ranges: []GranularityRange{
				{Min: 0.0, Max: 5.0, Increment: 0.1},
				{Min: 5.0, Max: 10.0, Increment: 1.0},
				{Min: 10.0, Max: 20.0, Increment: 5.0},
			},
		},
	},
}

func TestGranularityUnmarshalBad(t *testing.T) {
	tests := [][]byte{
		[]byte(`{}`),
		[]byte(`[]`),
		[]byte(`{"precision": -1, "ranges": [{"max":20, "increment":0.5}]}`),
		[]byte(`{"ranges":[{"max":20, "increment": -1}]}`),
		[]byte(`{"ranges":[{"max":"20", "increment": "0.1"}]}`),
		[]byte(`{"ranges":[{"max":20, "increment":0.1}. {"max":10, "increment":0.02}]}`),
		[]byte(`{"ranges":[{"max":20, "min":10, "increment": 0.1}, {"max":10, "min":0, "increment":0.05}]}`),
		[]byte(`{"ranges":[{"max":1.0, "increment": 0.07}, {"max" 1.0, "increment": 0.03}]}`),
	}
	var resolved PriceGranularity
	for _, b := range tests {
		resolved = PriceGranularity{}
		err := jsoniter.Unmarshal(b, &resolved)
		if err == nil {
			t.Errorf("Invalid granularity unmarshalled without error.\nJSON was: %s\n Resolved to: %v", string(b), resolved)
		}
	}
}
