package currencies

import (
	"errors"
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"
	"golang.org/x/text/currency"
)

// Rates holds data as represented on https://cdn.jsdelivr.net/gh/prebid/currency-file@1/latest.json
// note that `DataAsOfRaw` field is needed when parsing remote JSON as the date format if not standard and requires
// custom parsing to be properly set as Golang time.Time
type Rates struct {
	DataAsOf    time.Time                     `json:"dataAsOf"`
	Conversions map[string]map[string]float64 `json:"conversions"`
}

// NewRates creates a new Rates object holding currencies rates
func NewRates(dataAsOf time.Time, conversions map[string]map[string]float64) *Rates {
	return &Rates{
		DataAsOf:    dataAsOf,
		Conversions: conversions,
	}
}

// UnmarshalJSON unmarshal raw JSON bytes to Rates object
func (r *Rates) UnmarshalJSON(b []byte) error {
	c := &struct {
		DataAsOf    string                        `json:"dataAsOf"`
		Conversions map[string]map[string]float64 `json:"conversions"`
	}{}
	if err := jsoniter.Unmarshal(b, &c); err != nil {
		return err
	}

	r.Conversions = c.Conversions

	layout := "2006-01-02"
	if date, err := time.Parse(layout, c.DataAsOf); err == nil {
		r.DataAsOf = date
	}

	return nil
}

// GetRate returns the conversion rate between two currencies
// returns an error in case the conversion rate between the two given currencies is not in the currencies rates map
func (r *Rates) GetRate(from string, to string) (float64, error) {
	var err error
	fromUnit, err := currency.ParseISO(from)
	if err != nil {
		return 0, err
	}
	toUnit, err := currency.ParseISO(to)
	if err != nil {
		return 0, err
	}
	if fromUnit.String() == toUnit.String() {
		return 1, nil
	}
	if r.Conversions != nil {
		if conversion, present := r.Conversions[fromUnit.String()][toUnit.String()]; present == true {
			return conversion, err
		}

		return 0, fmt.Errorf("Currency conversion rate not found: '%s' => '%s'", fromUnit.String(), toUnit.String())
	}
	return 0, errors.New("rates are nil")
}

// GetRates returns current rates
func (r *Rates) GetRates() *map[string]map[string]float64 {
	return &r.Conversions
}
