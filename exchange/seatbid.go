package exchange

import "encoding/json"

// ExtSeatBid defines the contract for bidresponse.seatbid.Ext
type ExtSeatBid struct {
	Bidder json.RawMessage `json:"Bidder,omitempty"`
}
