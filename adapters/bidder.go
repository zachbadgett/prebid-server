package adapters

import (
	"context"
	"encoding/base64"
	"net/http"

	"github.com/prebid/prebid-server/errortypes"
	"github.com/prebid/prebid-server/openrtb_ext"

	"github.com/mxmCherry/openrtb"
)

// Bidder describes how to connect to external demand.
type Bidder interface {
	BidderName() openrtb_ext.BidderName

	// MakeRequests makes the HTTP requests which should be made to fetch Bids.
	//
	// nil return values are acceptable, but nil elements *inside* those slices are not.
	//
	// The errors should contain a list of errors which explain why this bidder's Bids will be
	// "subpar" in some way. For example: the request contained ad types which this bidder doesn't support.
	//
	// If the error is caused by bad user input, return a BadInputError.
	MakeRequests(request *openrtb.BidRequest) ([]*RequestData, []error)

	// MakeBids unpacks the server's response into Bids.
	//
	// The Bids can be nil (for no Bids), but should not contain nil elements.
	//
	// The errors should contain a list of errors which explain why this bidder's Bids will be
	// "subpar" in some way. For example: the server response didn't have the expected format.
	//
	// If the error was caused by bad user input, return a BadInputError.
	// If the error was caused by a bad server response, return a BadServerResponseError
	MakeBids(internalRequest *openrtb.BidRequest, externalRequest *RequestData, response *ResponseData) (*BidderResponse, []error)
}

type Requester interface {
	RequestBid(ctx context.Context, request *openrtb.BidRequest, name openrtb_ext.BidderName, bidAdjustment float64) (*SeatBid, []error)
}

func BadInput(msg string) *errortypes.BadInput {
	return &errortypes.BadInput{
		Message: msg,
	}
}

// BidderResponse wraps the server's response with the list of Bids and the currency used by the bidder.
//
// Currency declaration is not mandatory but helps to detect an eventual currency mismatch issue.
// From the Bid response, the bidder accepts a list of valid currencies for the Bid.
// The currency is the same accross all Bids.
type BidderResponse struct {
	Currency string
	Bids     []*TypedBid
}

// NewBidderResponseWithBidsCapacity create a new BidderResponse initialising the Bids array capacity and the default currency value
// to "USD".
//
// bidsCapacity allows to set initial Bids array capacity.
// By default, currency is USD but this behavior might be subject to change.
func NewBidderResponseWithBidsCapacity(bidsCapacity int) *BidderResponse {
	return &BidderResponse{
		Currency: "USD",
		Bids:     make([]*TypedBid, 0, bidsCapacity),
	}
}

// NewBidderResponse create a new BidderResponse initialising the Bids array and the default currency value
// to "USD".
//
// By default, bids capacity will be set to 0.
// By default, currency is USD but this behavior might be subject to change.
func NewBidderResponse() *BidderResponse {
	return NewBidderResponseWithBidsCapacity(0)
}

// TypedBid packages the openrtb.Bid with any bidder-specific information that PBS needs to populate an
// openrtb_ext.ExtBidPrebid.
//
// TypedBid.Bid.Ext will become "response.seatbid[i].Bid.Ext.bidder" in the final OpenRTB response.
// TypedBid.BidType will become "response.seatbid[i].Bid.Ext.prebid.type" in the final OpenRTB response.
type TypedBid struct {
	Bid     *openrtb.Bid
	BidType openrtb_ext.BidType
}

// RequestData and ResponseData exist so that prebid-server core code can implement its "debug" functionality
// uniformly across all Bidders.
// It will also let us experiment with valyala/vasthttp vs. net/http without changing every adapter (see #152)

// ResponseData packages together information from the server's http.Response.
type ResponseData struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// RequestData packages together the fields needed to make an http.Request.
type RequestData struct {
	Method  string
	Uri     string
	Body    []byte
	Headers http.Header
}

// ExtImpBidder can be used by Bidders to unmarshal any request.imp[i].Ext.
type ExtImpBidder struct {
	Prebid *openrtb_ext.ExtImpPrebid `json:"prebid"`

	// Bidder contain the bidder-specific extension. Each bidder should unmarshal this using their
	// corresponding openrtb_ext.ExtImp{Bidder} struct.
	//
	// For example, the Appnexus Bidder should unmarshal this with an openrtb_ext.ExtImpAppnexus object.
	//
	// Bidder implementations may safely assume that this JSON has been validated by their
	// static/bidder-params/{bidder}.json file.
	Bidder openrtb.RawJSON `json:"bidder"`
}

func (r *RequestData) SetBasicAuth(username string, password string) {
	r.Headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
}
