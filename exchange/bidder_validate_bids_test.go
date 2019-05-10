package exchange

import (
	"context"
	"testing"

	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/currencies"
	"github.com/prebid/prebid-server/openrtb_ext"
	"github.com/stretchr/testify/assert"
)

func TestAllValidBids(t *testing.T) {
	var bidder AdaptedBidder = ensureValidBids(&mockAdaptedBidder{
		bidResponse: &PBSOrtbSeatBid{
			Bids: []*PBSOrtbBid{
				{
					Bid: &openrtb.Bid{
						ID:    "one-Bid",
						ImpID: "thisImp",
						Price: 0.45,
						CrID:  "thisCreative",
					},
				},
				{
					Bid: &openrtb.Bid{
						ID:    "thatBid",
						ImpID: "thatImp",
						Price: 0.40,
						CrID:  "thatCreative",
					},
				},
				{
					Bid: &openrtb.Bid{
						ID:    "123",
						ImpID: "456",
						Price: 0.44,
						CrID:  "789",
					},
				},
			},
		},
	})
	seatBid, errs := bidder.RequestBid(context.Background(), &openrtb.BidRequest{}, openrtb_ext.BidderAppnexus, 1.0, currencies.NewConstantRates())
	assert.Len(t, seatBid.Bids, 3)
	assert.Len(t, errs, 0)
}

func TestAllBadBids(t *testing.T) {
	bidder := ensureValidBids(&mockAdaptedBidder{
		bidResponse: &PBSOrtbSeatBid{
			Bids: []*PBSOrtbBid{
				{
					Bid: &openrtb.Bid{
						ID:    "one-Bid",
						Price: 0.45,
						CrID:  "thisCreative",
					},
				},
				{
					Bid: &openrtb.Bid{
						ID:    "thatBid",
						ImpID: "thatImp",
						CrID:  "thatCreative",
					},
				},
				{
					Bid: &openrtb.Bid{
						ID:    "123",
						ImpID: "456",
						Price: 0.44,
					},
				},
				{
					Bid: &openrtb.Bid{
						ImpID: "456",
						Price: 0.44,
						CrID:  "blah",
					},
				},
				{},
			},
		},
	})
	seatBid, errs := bidder.RequestBid(context.Background(), &openrtb.BidRequest{}, openrtb_ext.BidderAppnexus, 1.0, currencies.NewConstantRates())
	assert.Len(t, seatBid.Bids, 0)
	assert.Len(t, errs, 5)
}

func TestMixedBids(t *testing.T) {
	bidder := ensureValidBids(&mockAdaptedBidder{
		bidResponse: &PBSOrtbSeatBid{
			Bids: []*PBSOrtbBid{
				{
					Bid: &openrtb.Bid{
						ID:    "one-Bid",
						ImpID: "thisImp",
						Price: 0.45,
						CrID:  "thisCreative",
					},
				},
				{
					Bid: &openrtb.Bid{
						ID:    "thatBid",
						ImpID: "thatImp",
						CrID:  "thatCreative",
					},
				},
				{
					Bid: &openrtb.Bid{
						ID:    "123",
						ImpID: "456",
						Price: 0.44,
						CrID:  "789",
					},
				},
				{
					Bid: &openrtb.Bid{
						ImpID: "456",
						Price: 0.44,
						CrID:  "blah",
					},
				},
				{},
			},
		},
	})
	seatBid, errs := bidder.RequestBid(context.Background(), &openrtb.BidRequest{}, openrtb_ext.BidderAppnexus, 1.0, currencies.NewConstantRates())
	assert.Len(t, seatBid.Bids, 2)
	assert.Len(t, errs, 3)
}

func TestCurrencyBids(t *testing.T) {
	currencyTestCases := []struct {
		brqCur           []string
		brpCur           string
		defaultCur       string
		expectedValidBid bool
	}{
		// Case Bid request and Bid response don't specify any currencies.
		// Expected to be valid since both Bid request / response will be overridden with default Currency (USD).
		{
			brqCur:           []string{},
			brpCur:           "",
			expectedValidBid: true,
		},
		// Case Bid request specifies a Currency (default one) but Bid response doesn't.
		// Expected to be valid since Bid response will be overridden with default Currency (USD).
		{
			brqCur:           []string{"USD"},
			brpCur:           "",
			expectedValidBid: true,
		},
		// Case Bid request specifies more than 1 Currency (default one and another one) but Bid response doesn't.
		// Expected to be valid since Bid response will be overridden with default Currency (USD).
		{
			brqCur:           []string{"USD", "EUR"},
			brpCur:           "",
			expectedValidBid: true,
		},
		// Case Bid request specifies more than 1 Currency (default one and another one) and Bid response specifies default Currency (USD).
		// Expected to be valid.
		{
			brqCur:           []string{"USD", "EUR"},
			brpCur:           "USD",
			expectedValidBid: true,
		},
		// Case Bid request specifies more than 1 Currency (default one and another one) and Bid response specifies the second Currency allowed (not USD).
		// Expected to be valid.
		{
			brqCur:           []string{"USD", "EUR"},
			brpCur:           "EUR",
			expectedValidBid: true,
		},
		// Case Bid request specifies only 1 Currency which is not the default one.
		// Bid response doesn't specify any Currency.
		// Expected to be invalid.
		{
			brqCur:           []string{"JPY"},
			brpCur:           "",
			expectedValidBid: false,
		},
		// Case Bid request doesn't specify any currencies.
		// Bid response specifies a Currency which is not the default one.
		// Expected to be invalid.
		{
			brqCur:           []string{},
			brpCur:           "JPY",
			expectedValidBid: false,
		},
		// Case Bid request specifies a Currency.
		// Bid response specifies a Currency which is not the one specified in Bid request.
		// Expected to be invalid.
		{
			brqCur:           []string{"USD"},
			brpCur:           "EUR",
			expectedValidBid: false,
		},
		// Case Bid request specifies several currencies.
		// Bid response specifies a Currency which is not the one specified in Bid request.
		// Expected to be invalid.
		{
			brqCur:           []string{"USD", "EUR"},
			brpCur:           "JPY",
			expectedValidBid: false,
		},
	}

	for _, tc := range currencyTestCases {
		bids := []*PBSOrtbBid{
			{
				Bid: &openrtb.Bid{
					ID:    "one-Bid",
					ImpID: "thisImp",
					Price: 0.45,
					CrID:  "thisCreative",
				},
			},
			{
				Bid: &openrtb.Bid{
					ID:    "thatBid",
					ImpID: "thatImp",
					Price: 0.44,
					CrID:  "thatCreative",
				},
			},
		}
		bidder := ensureValidBids(&mockAdaptedBidder{
			bidResponse: &PBSOrtbSeatBid{
				Currency: tc.brpCur,
				Bids:     bids,
			},
		})

		expectedValidBids := len(bids)
		expectedErrs := 0

		if tc.expectedValidBid != true {
			// If Currency mistmatch, we should have one error
			expectedErrs = 1
			expectedValidBids = 0
		}

		request := &openrtb.BidRequest{
			Cur: tc.brqCur,
		}

		seatBid, errs := bidder.RequestBid(context.Background(), request, openrtb_ext.BidderAppnexus, 1.0, currencies.NewConstantRates())
		assert.Len(t, seatBid.Bids, expectedValidBids)
		assert.Len(t, errs, expectedErrs)
	}
}

type mockAdaptedBidder struct {
	bidResponse   *PBSOrtbSeatBid
	errorResponse []error
}

func (b *mockAdaptedBidder) RequestBid(ctx context.Context, request *openrtb.BidRequest, name openrtb_ext.BidderName, bidAdjustment float64, conversions currencies.Conversions) (*PBSOrtbSeatBid, []error) {
	return b.bidResponse, b.errorResponse
}
