package adapters

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/errortypes"
	"github.com/prebid/prebid-server/openrtb_ext"

	"golang.org/x/net/context/ctxhttp"
)

// Bid is a bid returned by an adaptedBidder.
//
// Bid.Bid.Ext will become "response.seatbid[i].Bid.Ext.bidder" in the final OpenRTB response.
// Bid.BidType will become "response.seatbid[i].Bid.Ext.prebid.type" in the final OpenRTB response.
// Bid.BidTargets does not need to be filled out by the Bidder. It will be set later by the exchange.d
type Bid struct {
	Bid        *openrtb.Bid
	BidType    openrtb_ext.BidType
	BidTargets map[string]string
}

// SeatBid is a bid returned by an adaptedBidder.
//
// This is distinct from the openrtb.SeatBid so that the prebid-server Ext can be passed back with typesafety.
type SeatBid struct {
	// Bids is the list of bids which this adaptedBidder wishes to make.
	Bids []*Bid
	// Currency is the currency in which the Bids are made.
	// Should be a valid currency ISO code.
	Currency string
	// HttpCalls is the list of debugging info. It should only be populated if the request.test == 1.
	// This will become response.Ext.debug.httpcalls.{bidder} on the final Response.
	HttpCalls []*openrtb_ext.ExtHttpCall
	// Ext contains the extension for this seatbid.
	// if len(Bids) > 0, this will become response.seatbid[i].Ext.{bidder} on the final OpenRTB response.
	// if len(Bids) == 0, this will be ignored because the OpenRTB spec doesn't allow a SeatBid with 0 Bids.
	Ext openrtb.RawJSON
}

type BidRequester struct {
	Bidder
	Client *http.Client
}

func NewBidRequester(bidder Bidder, client *http.Client) *BidRequester {
	return &BidRequester{
		Bidder: bidder,
		Client: client,
	}
}

func (bidder *BidRequester) RequestBid(ctx context.Context, request *openrtb.BidRequest, name openrtb_ext.BidderName, bidAdjustment float64) (*SeatBid, []error) {
	reqData, errs := bidder.Bidder.MakeRequests(request)
	if len(reqData) == 0 {
		// If the adapter failed to generate both requests and errors, this is an error.
		if len(errs) == 0 {
			errs = append(errs, &errortypes.FailedToRequestBids{Message: "The adapter failed to generate any bid requests, but also failed to generate an error explaining why"})
		}
		return nil, errs
	}
	// Make any HTTP requests in parallel.
	// If the bidder only needs to make one, save some cycles by just using the current one.
	responseChannel := make(chan *httpCallInfo, len(reqData))
	if len(reqData) == 1 {
		responseChannel <- bidder.doRequest(ctx, reqData[0])
	} else {
		for _, oneReqData := range reqData {
			go func(data *RequestData) {
				responseChannel <- bidder.doRequest(ctx, data)
			}(oneReqData) // Method arg avoids a race condition on oneReqData
		}
	}
	seatBid := &SeatBid{
		Bids:      make([]*Bid, 0, len(reqData)),
		Currency:  "USD",
		HttpCalls: make([]*openrtb_ext.ExtHttpCall, 0, len(reqData)),
	}
	firstHTTPCallCurrency := ""
	// If the bidder made multiple requests, we still want them to enter as many bids as possible...
	// even if the timeout occurs sometime halfway through.
	for i := 0; i < len(reqData); i++ {
		httpInfo := <-responseChannel
		// If this is a test bid, capture debugging info from the requests.
		if request.Test == 1 {
			seatBid.HttpCalls = append(seatBid.HttpCalls, makeExt(httpInfo))
		}
		if httpInfo.err == nil {
			bidResponse, moreErrs := bidder.Bidder.MakeBids(request, httpInfo.request, httpInfo.response)
			errs = append(errs, moreErrs...)
			if bidResponse != nil {
				if bidResponse.Currency == "" {
					bidResponse.Currency = "USD"
				}
				// Related to #281 - Currency support
				// Prebid can't make sure that each HTTP call returns Bids with the same currency as the others.
				// If a bidder makes two HTTP calls, and their servers respond with different currencies,
				// we will consider the first call currency as standard currency and then reject others which contradict it.
				if firstHTTPCallCurrency == "" { // First HTTP call
					firstHTTPCallCurrency = bidResponse.Currency
				}
				// TODO: #281 - Once currencies rate conversion is out, this shouldn't be an issue anymore, we will only
				// need to convert the bid price based on the currency.
				if firstHTTPCallCurrency == bidResponse.Currency {
					for i := 0; i < len(bidResponse.Bids); i++ {
						if bidResponse.Bids[i].Bid != nil {
							// TODO #280: Convert the Bid price
							bidResponse.Bids[i].Bid.Price = bidResponse.Bids[i].Bid.Price * bidAdjustment
						}
						seatBid.Bids = append(seatBid.Bids, &Bid{
							Bid:     bidResponse.Bids[i].Bid,
							BidType: bidResponse.Bids[i].BidType,
						})
					}
				} else {
					errs = append(errs, fmt.Errorf(
						"Bid currencies mistmatch found. Expected all bids to have the same currencies. Expected '%s', was: '%s'",
						firstHTTPCallCurrency,
						bidResponse.Currency,
					))
				}
			}
		} else {
			errs = append(errs, httpInfo.err)
		}
	}

	return seatBid, errs
}

// makeExt transforms information about the HTTP call into the contract class for the PBS response.
func makeExt(httpInfo *httpCallInfo) *openrtb_ext.ExtHttpCall {
	if httpInfo.err == nil {
		return &openrtb_ext.ExtHttpCall{
			Uri:          httpInfo.request.Uri,
			RequestBody:  string(httpInfo.request.Body),
			ResponseBody: string(httpInfo.response.Body),
			Status:       httpInfo.response.StatusCode,
		}
	} else if httpInfo.request == nil {
		return &openrtb_ext.ExtHttpCall{}
	} else {
		return &openrtb_ext.ExtHttpCall{
			Uri:         httpInfo.request.Uri,
			RequestBody: string(httpInfo.request.Body),
		}
	}
}

// doRequest makes a request, handles the response, and returns the data needed by the
// Bidder interface.
func (bidder *BidRequester) doRequest(ctx context.Context, req *RequestData) *httpCallInfo {
	httpReq, err := http.NewRequest(req.Method, req.Uri, bytes.NewBuffer(req.Body))
	if err != nil {
		return &httpCallInfo{
			request: req,
			err:     err,
		}
	}
	httpReq.Header = req.Headers

	httpResp, err := ctxhttp.Do(ctx, bidder.Client, httpReq)
	if err != nil {
		if err == context.DeadlineExceeded {
			err = &errortypes.Timeout{Message: err.Error()}
		}
		return &httpCallInfo{
			request: req,
			err:     err,
		}
	}

	respBody, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return &httpCallInfo{
			request: req,
			err:     err,
		}
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 400 {
		err = &errortypes.BadServerResponse{
			Message: fmt.Sprintf("Server responded with failure status: %d. Set request.test = 1 for debugging info.", httpResp.StatusCode),
		}
	}

	return &httpCallInfo{
		request: req,
		response: &ResponseData{
			StatusCode: httpResp.StatusCode,
			Body:       respBody,
			Headers:    httpResp.Header,
		},
		err: err,
	}
}

type httpCallInfo struct {
	request  *RequestData
	response *ResponseData
	err      error
}
