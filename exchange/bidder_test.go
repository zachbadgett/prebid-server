package exchange

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prebid/prebid-server/adcert"

	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/openrtb_ext"
)

// TestSingleBidder makes sure that the following things work if the Bidder needs only one request.
//
// 1. The Bidder implementation is called with the arguments we expect.
// 2. The returned values are correct for a non-test bid.
func TestSingleBidder(t *testing.T) {
	respStatus := 200
	respBody := "{\"bid\":false}"
	server := httptest.NewServer(mockHandler(respStatus, "getBody", respBody))
	defer server.Close()

	requestHeaders := http.Header{}
	requestHeaders.Add("Content-Type", "application/json")

	bidAdjustment := 2.0
	firstInitialPrice := 3.0
	secondInitialPrice := 4.0
	mockBidderResponse := &adapters.BidderResponse{
		Bids: []*adapters.TypedBid{
			{
				Bid: &openrtb.Bid{
					Price: firstInitialPrice,
				},
				BidType: openrtb_ext.BidTypeBanner,
			},
			{
				Bid: &openrtb.Bid{
					Price: secondInitialPrice,
				},
				BidType: openrtb_ext.BidTypeVideo,
			},
		},
	}

	bidderImpl := &goodSingleBidder{
		httpRequest: &adapters.RequestData{
			Method:  "POST",
			Uri:     server.URL,
			Body:    []byte("{\"key\":\"val\"}"),
			Headers: http.Header{},
		},
		bidResponse: mockBidderResponse,
	}
	bidder := adaptBidder(bidderImpl, server.Client())
	seatBid, errs := bidder.requestBid(context.Background(), &adcert.BidRequest{BidRequest: &openrtb.BidRequest{}}, "test", bidAdjustment)

	// Make sure the goodSingleBidder was called with the expected arguments.
	if bidderImpl.httpResponse == nil {
		t.Errorf("The Bidder should be called with the server's response.")
	}
	if bidderImpl.httpResponse.StatusCode != respStatus {
		t.Errorf("Bad response status. Expected %d, got %d", respStatus, bidderImpl.httpResponse.StatusCode)
	}
	if string(bidderImpl.httpResponse.Body) != respBody {
		t.Errorf("Bad response body. Expected %s, got %s", respBody, string(bidderImpl.httpResponse.Body))
	}

	// Make sure the returned values are what we expect
	if len(errs) != 0 {
		t.Errorf("bidder.Bid returned %d errors. Expected 0", len(errs))
	}
	if len(seatBid.bids) != len(mockBidderResponse.Bids) {
		t.Fatalf("Expected %d bids. Got %d", len(mockBidderResponse.Bids), len(seatBid.bids))
	}
	for index, typedBid := range mockBidderResponse.Bids {
		if typedBid.Bid != seatBid.bids[index].bid {
			t.Errorf("Bid %d did not point to the same bid returned by the Bidder.", index)
		}
		if typedBid.BidType != seatBid.bids[index].bidType {
			t.Errorf("Bid %d did not have the right type. Expected %s, got %s", index, typedBid.BidType, seatBid.bids[index].bidType)
		}
	}
	if mockBidderResponse.Bids[0].Bid.Price != bidAdjustment*firstInitialPrice {
		t.Errorf("Bid[0].Price was not adjusted properly. Expected %f, got %f", bidAdjustment*firstInitialPrice, mockBidderResponse.Bids[0].Bid.Price)
	}
	if mockBidderResponse.Bids[1].Bid.Price != bidAdjustment*secondInitialPrice {
		t.Errorf("Bid[1].Price was not adjusted properly. Expected %f, got %f", bidAdjustment*secondInitialPrice, mockBidderResponse.Bids[1].Bid.Price)
	}
	if len(seatBid.httpCalls) != 0 {
		t.Errorf("The bidder shouldn't log HttpCalls when request.test == 0. Found %d", len(seatBid.httpCalls))
	}

	if len(seatBid.ext) != 0 {
		t.Errorf("The bidder shouldn't define any seatBid.ext. Got %s", string(seatBid.ext))
	}
}

// TestMultiBidder makes sure all the requests get sent, and the responses processed.
// Because this is done in parallel, it should be run under the race detector.
func TestMultiBidder(t *testing.T) {
	respStatus := 200
	getRespBody := "{\"wasPost\":false}"
	postRespBody := "{\"wasPost\":true}"
	server := httptest.NewServer(mockHandler(respStatus, getRespBody, postRespBody))
	defer server.Close()

	requestHeaders := http.Header{}
	requestHeaders.Add("Content-Type", "application/json")

	mockBidderResponse := &adapters.BidderResponse{
		Bids: []*adapters.TypedBid{
			{
				Bid:     &openrtb.Bid{},
				BidType: openrtb_ext.BidTypeBanner,
			},
			{
				Bid:     &openrtb.Bid{},
				BidType: openrtb_ext.BidTypeVideo,
			},
		},
	}

	bidderImpl := &mixedMultiBidder{
		httpRequests: []*adapters.RequestData{{
			Method:  "POST",
			Uri:     server.URL,
			Body:    []byte("{\"key\":\"val\"}"),
			Headers: http.Header{},
		},
			{
				Method:  "GET",
				Uri:     server.URL,
				Body:    []byte("{\"key\":\"val2\"}"),
				Headers: http.Header{},
			}},
		bidResponse: mockBidderResponse,
	}
	bidder := adaptBidder(bidderImpl, server.Client())
	seatBid, errs := bidder.requestBid(context.Background(), &adcert.BidRequest{BidRequest: &openrtb.BidRequest{}}, "test", 1.0)

	if seatBid == nil {
		t.Fatalf("SeatBid should exist, because bids exist.")
	}

	if len(errs) != 1+len(bidderImpl.httpRequests) {
		t.Errorf("Expected %d errors. Got %d", 1+len(bidderImpl.httpRequests), len(errs))
	}
	if len(seatBid.bids) != len(bidderImpl.httpResponses)*len(mockBidderResponse.Bids) {
		t.Errorf("Expected %d bids. Got %d", len(bidderImpl.httpResponses)*len(mockBidderResponse.Bids), len(seatBid.bids))
	}

}

// TestBidderTimeout makes sure that things work smoothly if the context expires before the Bidder
// manages to complete its task.
func TestBidderTimeout(t *testing.T) {
	// Fixes #369 (hopefully): Define a context which has already expired
	ctx, cancelFunc := context.WithDeadline(context.Background(), time.Now().Add(-7*time.Hour))
	cancelFunc()
	<-ctx.Done()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		if r.Method == "GET" {
			w.Write([]byte("getBody"))
		} else {
			w.Write([]byte("postBody"))
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	bidder := &bidderAdapter{
		Bidder: &mixedMultiBidder{},
		Client: server.Client(),
	}

	callInfo := bidder.doRequest(ctx, &adapters.RequestData{
		Method: "POST",
		Uri:    server.URL,
	})
	if callInfo.err == nil {
		t.Errorf("The bidder should report an error if the context has expired already.")
	}
	if callInfo.response != nil {
		t.Errorf("There should be no response if the request never completed.")
	}
}

// TestInvalidRequest makes sure that bidderAdapter.doRequest returns errors on bad requests.
func TestInvalidRequest(t *testing.T) {
	server := httptest.NewServer(mockHandler(200, "getBody", "postBody"))
	bidder := &bidderAdapter{
		Bidder: &mixedMultiBidder{},
		Client: server.Client(),
	}

	callInfo := bidder.doRequest(context.Background(), &adapters.RequestData{
		Method: "\"", // force http.NewRequest() to fail
	})
	if callInfo.err == nil {
		t.Errorf("bidderAdapter.doRequest should return an error if the request data is malformed.")
	}
}

// TestConnectionClose makes sure that bidderAdapter.doRequest returns errors if the connection closes unexpectedly.
func TestConnectionClose(t *testing.T) {
	var server *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.CloseClientConnections()
	})
	server = httptest.NewServer(handler)

	bidder := &bidderAdapter{
		Bidder: &mixedMultiBidder{},
		Client: server.Client(),
	}

	callInfo := bidder.doRequest(context.Background(), &adapters.RequestData{
		Method: "POST",
		Uri:    server.URL,
	})
	if callInfo.err == nil {
		t.Errorf("bidderAdapter.doRequest should return an error if the connection closes unexpectedly.")
	}
}

// TestMultiCurrencies makes sure that bidderAdapter.requestBid returns errors if the bidder pass several currencies in case of multi HTTP calls.
func TestMultiCurrencies(t *testing.T) {
	// Setup:
	respStatus := 200
	getRespBody := "{\"wasPost\":false}"
	postRespBody := "{\"wasPost\":true}"

	testCases := []struct {
		bidRequestCurrencies           []string
		bidCurrency                    []string
		expectedBidsCount              uint
		expectedBadCurrencyErrorsCount uint
	}{
		// Bidder respond with the same currency (default one) on all HTTP responses
		{
			bidCurrency:                    []string{"USD", "USD", "USD"},
			expectedBidsCount:              3,
			expectedBadCurrencyErrorsCount: 0,
		},
		// Bidder respond with the same currency (not default one) on all HTTP responses
		{
			bidCurrency:                    []string{"EUR", "EUR", "EUR"},
			expectedBidsCount:              3,
			expectedBadCurrencyErrorsCount: 0,
		},
		// Bidder responds with currency not set on all HTTP responses
		{
			bidCurrency:                    []string{"", "", ""},
			expectedBidsCount:              3,
			expectedBadCurrencyErrorsCount: 0,
		},
		// Bidder responds with a mix of not set and default currency in HTTP responses
		{
			bidCurrency:                    []string{"", "USD", ""},
			expectedBidsCount:              3,
			expectedBadCurrencyErrorsCount: 0,
		},
		// Bidder responds with a mix of not set and default currency in HTTP responses
		{
			bidCurrency:                    []string{"USD", "USD", ""},
			expectedBidsCount:              3,
			expectedBadCurrencyErrorsCount: 0,
		},
		// Bidder responds with a mix of not set and default currency in HTTP responses
		{
			bidCurrency:                    []string{"", "", "USD"},
			expectedBidsCount:              3,
			expectedBadCurrencyErrorsCount: 0,
		},
		// Bidder responds with a mix of not set, non default currency and default currency in HTTP responses
		{
			bidCurrency:                    []string{"EUR", "", "USD"},
			expectedBidsCount:              1,
			expectedBadCurrencyErrorsCount: 2,
		},
		// Bidder responds with a mix of not set, non default currency and default currency in HTTP responses
		{
			bidCurrency:                    []string{"GDB", "", "USD"},
			expectedBidsCount:              1,
			expectedBadCurrencyErrorsCount: 2,
		},
		// Bidder responds with a mix of not set and default currency in HTTP responses
		{
			bidCurrency:                    []string{"GDB", "", ""},
			expectedBidsCount:              1,
			expectedBadCurrencyErrorsCount: 2,
		},
		// Bidder responds with a mix of not set and default currency in HTTP responses
		{
			bidCurrency:                    []string{"GDB", "GDB", "GDB"},
			expectedBidsCount:              3,
			expectedBadCurrencyErrorsCount: 0,
		},
	}

	server := httptest.NewServer(mockHandler(respStatus, getRespBody, postRespBody))
	defer server.Close()

	for _, tc := range testCases {
		mockBidderResponses := make([]*adapters.BidderResponse, len(tc.bidCurrency))
		bidderImpl := &goodMultiHTTPCallsBidder{
			bidResponses: mockBidderResponses,
		}
		bidderImpl.httpRequest = make([]*adapters.RequestData, len(tc.bidCurrency))

		for i, cur := range tc.bidCurrency {

			mockBidderResponses[i] = &adapters.BidderResponse{
				Bids: []*adapters.TypedBid{
					{
						Bid:     &openrtb.Bid{},
						BidType: openrtb_ext.BidTypeBanner,
					},
				},
				Currency: cur,
			}

			bidderImpl.httpRequest[i] = &adapters.RequestData{
				Method:  "POST",
				Uri:     server.URL,
				Body:    []byte("{\"key\":\"val\"}"),
				Headers: http.Header{},
			}
		}

		// Execute:
		bidder := adaptBidder(bidderImpl, server.Client())
		seatBid, errs := bidder.requestBid(
			context.Background(),
			&adcert.BidRequest{BidRequest: &openrtb.BidRequest{}},
			"test",
			1,
		)

		// Verify:
		if seatBid == nil && tc.expectedBidsCount != 0 {
			t.Errorf("Seatbid is nil but expected not to be nil")
		}
		if tc.expectedBidsCount != uint(len(seatBid.bids)) {
			t.Errorf("Expected to have %d bids count but got %d", tc.expectedBidsCount, len(seatBid.bids))
		}
		if tc.expectedBadCurrencyErrorsCount != uint(len(errs)) {
			t.Errorf("Expected to have %d errors count but got %d", tc.expectedBadCurrencyErrorsCount, len(errs))
		}

	}
}

// TestBadResponseLogging makes sure that openrtb_ext works properly on malformed HTTP requests.
func TestBadRequestLogging(t *testing.T) {
	info := &httpCallInfo{
		err: errors.New("Bad request"),
	}
	ext := makeExt(info)
	if ext.Uri != "" {
		t.Errorf("The URI should be empty. Got %s", ext.Uri)
	}
	if ext.RequestBody != "" {
		t.Errorf("The request body should be empty. Got %s", ext.RequestBody)
	}
	if ext.ResponseBody != "" {
		t.Errorf("The response body should be empty. Got %s", ext.ResponseBody)
	}
	if ext.Status != 0 {
		t.Errorf("The Status code should be 0. Got %d", ext.Status)
	}
}

// TestBadResponseLogging makes sure that openrtb_ext works properly if we don't get a sensible HTTP response.
func TestBadResponseLogging(t *testing.T) {
	info := &httpCallInfo{
		request: &adapters.RequestData{
			Uri:  "test.com",
			Body: []byte("request body"),
		},
		err: errors.New("Bad response"),
	}
	ext := makeExt(info)
	if ext.Uri != info.request.Uri {
		t.Errorf("The URI should be test.com. Got %s", ext.Uri)
	}
	if ext.RequestBody != string(info.request.Body) {
		t.Errorf("The request body should be empty. Got %s", ext.RequestBody)
	}
	if ext.ResponseBody != "" {
		t.Errorf("The response body should be empty. Got %s", ext.ResponseBody)
	}
	if ext.Status != 0 {
		t.Errorf("The Status code should be 0. Got %d", ext.Status)
	}
}

// TestSuccessfulResponseLogging makes sure that openrtb_ext works properly if the HTTP request is successful.
func TestSuccessfulResponseLogging(t *testing.T) {
	info := &httpCallInfo{
		request: &adapters.RequestData{
			Uri:  "test.com",
			Body: []byte("request body"),
		},
		response: &adapters.ResponseData{
			StatusCode: 200,
			Body:       []byte("response body"),
		},
	}
	ext := makeExt(info)
	if ext.Uri != info.request.Uri {
		t.Errorf("The URI should be test.com. Got %s", ext.Uri)
	}
	if ext.RequestBody != string(info.request.Body) {
		t.Errorf("The request body should be \"request body\". Got %s", ext.RequestBody)
	}
	if ext.ResponseBody != string(info.response.Body) {
		t.Errorf("The response body should be \"response body\". Got %s", ext.ResponseBody)
	}
	if ext.Status != info.response.StatusCode {
		t.Errorf("The Status code should be 0. Got %d", ext.Status)
	}
}

// TestServerCallDebugging makes sure that we log the server calls made by the Bidder on test bids.
func TestServerCallDebugging(t *testing.T) {
	respBody := "{\"bid\":false}"
	respStatus := 200
	server := httptest.NewServer(mockHandler(respStatus, "getBody", respBody))
	defer server.Close()

	reqBody := "{\"key\":\"val\"}"
	reqUrl := server.URL
	bidderImpl := &goodSingleBidder{
		httpRequest: &adapters.RequestData{
			Method:  "POST",
			Uri:     reqUrl,
			Body:    []byte(reqBody),
			Headers: http.Header{},
		},
	}
	bidder := adaptBidder(bidderImpl, server.Client())

	bids, _ := bidder.requestBid(context.Background(), &adcert.BidRequest{
		BidRequest: &openrtb.BidRequest{
			Test: 1,
		},
	}, "test", 1.0)

	if len(bids.httpCalls) != 1 {
		t.Errorf("We should log the server call if this is a test bid. Got %d", len(bids.httpCalls))
	}
	if bids.httpCalls[0].Uri != reqUrl {
		t.Errorf("Wrong httpcalls URI. Expected %s, got %s", reqUrl, bids.httpCalls[0].Uri)
	}
	if bids.httpCalls[0].RequestBody != reqBody {
		t.Errorf("Wrong httpcalls RequestBody. Expected %s, got %s", reqBody, bids.httpCalls[0].RequestBody)
	}
	if bids.httpCalls[0].ResponseBody != respBody {
		t.Errorf("Wrong httpcalls ResponseBody. Expected %s, got %s", respBody, bids.httpCalls[0].ResponseBody)
	}
	if bids.httpCalls[0].Status != respStatus {
		t.Errorf("Wrong httpcalls Status. Expected %d, got %d", respStatus, bids.httpCalls[0].Status)
	}
}

func TestErrorReporting(t *testing.T) {
	bidder := adaptBidder(&bidRejector{}, nil)
	bids, errs := bidder.requestBid(context.Background(), &adcert.BidRequest{BidRequest: &openrtb.BidRequest{}}, "test", 1.0)
	if bids != nil {
		t.Errorf("There should be no seatbid if no http requests are returned.")
	}
	if len(errs) != 1 {
		t.Fatalf("Expected 1 error. got %d", len(errs))
	}
	if errs[0].Error() != "Invalid params on BidRequest." {
		t.Errorf(`Error message was mutated. Expected "%s", Got "%s"`, "Invalid params on BidRequest.", errs[0].Error())
	}
}

type goodSingleBidder struct {
	bidRequest   *adcert.BidRequest
	httpRequest  *adapters.RequestData
	httpResponse *adapters.ResponseData
	bidResponse  *adapters.BidderResponse
}

func (bidder *goodSingleBidder) MakeRequests(request *adcert.BidRequest) ([]*adapters.RequestData, []error) {
	bidder.bidRequest = request
	return []*adapters.RequestData{bidder.httpRequest}, nil
}

func (bidder *goodSingleBidder) MakeBids(internalRequest *adcert.BidRequest, externalRequest *adapters.RequestData, response *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	bidder.httpResponse = response
	return bidder.bidResponse, nil
}

type goodMultiHTTPCallsBidder struct {
	bidRequest        *adcert.BidRequest
	httpRequest       []*adapters.RequestData
	httpResponses     []*adapters.ResponseData
	bidResponses      []*adapters.BidderResponse
	bidResponseNumber int
}

func (bidder *goodMultiHTTPCallsBidder) MakeRequests(request *adcert.BidRequest) ([]*adapters.RequestData, []error) {
	bidder.bidRequest = request
	response := make([]*adapters.RequestData, len(bidder.httpRequest))

	for i, r := range bidder.httpRequest {
		response[i] = r
	}
	return response, nil
}

func (bidder *goodMultiHTTPCallsBidder) MakeBids(internalRequest *adcert.BidRequest, externalRequest *adapters.RequestData, response *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	br := bidder.bidResponses[bidder.bidResponseNumber]
	bidder.bidResponseNumber++
	bidder.httpResponses = append(bidder.httpResponses, response)

	return br, nil
}

type mixedMultiBidder struct {
	bidRequest    *adcert.BidRequest
	httpRequests  []*adapters.RequestData
	httpResponses []*adapters.ResponseData
	bidResponse   *adapters.BidderResponse
}

func (bidder *mixedMultiBidder) MakeRequests(request *adcert.BidRequest) ([]*adapters.RequestData, []error) {
	bidder.bidRequest = request
	return bidder.httpRequests, []error{errors.New("The requests weren't ideal.")}
}

func (bidder *mixedMultiBidder) MakeBids(internalRequest *adcert.BidRequest, externalRequest *adapters.RequestData, response *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	bidder.httpResponses = append(bidder.httpResponses, response)
	return bidder.bidResponse, []error{errors.New("The bidResponse weren't ideal.")}
}

type bidRejector struct {
	httpRequest  *adapters.RequestData
	httpResponse *adapters.ResponseData
}

func (bidder *bidRejector) MakeRequests(request *adcert.BidRequest) ([]*adapters.RequestData, []error) {
	return nil, []error{errors.New("Invalid params on BidRequest.")}
}

func (bidder *bidRejector) MakeBids(internalRequest *adcert.BidRequest, externalRequest *adapters.RequestData, response *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	bidder.httpResponse = response
	return nil, []error{errors.New("Can't make a response.")}
}
