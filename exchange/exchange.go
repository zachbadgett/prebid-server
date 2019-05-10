package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"runtime/debug"
	"sort"
	"time"

	"github.com/prebid/prebid-server/stored_requests"

	"github.com/golang/glog"
	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/currencies"
	"github.com/prebid/prebid-server/errortypes"
	"github.com/prebid/prebid-server/gdpr"
	"github.com/prebid/prebid-server/openrtb_ext"
	"github.com/prebid/prebid-server/pbsmetrics"
	"github.com/prebid/prebid-server/prebid_cache_client"
)

// Exchange runs Auctions. Implementations must be threadsafe, and will be shared across many goroutines.
type Exchange interface {
	// HoldAuction executes an OpenRTB v2.5 Auction.
	HoldAuction(ctx context.Context, bidRequest *openrtb.BidRequest, usersyncs IdFetcher, labels pbsmetrics.Labels, categoriesFetcher *stored_requests.CategoryFetcher) (*openrtb.BidResponse, error)
	RecoverSafely(inner func(openrtb_ext.BidderName, openrtb_ext.BidderName, *openrtb.BidRequest, *pbsmetrics.AdapterLabels, currencies.Conversions), chBids chan *BidResponseWrapper) func(openrtb_ext.BidderName, openrtb_ext.BidderName, *openrtb.BidRequest, *pbsmetrics.AdapterLabels, currencies.Conversions)
}

// IdFetcher can find the user's ID for a specific Bidder.
type IdFetcher interface {
	// GetId returns the ID for the Bidder. The boolean will be true if the ID exists, and false otherwise.
	GetId(bidder openrtb_ext.BidderName) (string, bool)
}

type exchange struct {
	adapterMap          map[openrtb_ext.BidderName]AdaptedBidder
	me                  pbsmetrics.MetricsEngine
	cache               prebid_cache_client.Client
	cacheTime           time.Duration
	gDPR                gdpr.Permissions
	currencyConverter   *currencies.RateConverter
	UsersyncIfAmbiguous bool
	defaultTTLs         config.DefaultTTLs
}

// Container to pass out response Ext data from the GetAllBids goroutines back into the main thread
type SeatResponseExtra struct {
	ResponseTimeMillis int
	Errors             []openrtb_ext.ExtBidderError
}

type BidResponseWrapper struct {
	AdapterBids  *PBSOrtbSeatBid
	AdapterExtra *SeatResponseExtra
	Bidder       openrtb_ext.BidderName
}

func NewExchange(client *http.Client, cache prebid_cache_client.Client, cfg *config.Configuration, metricsEngine pbsmetrics.MetricsEngine, infos adapters.BidderInfos, gDPR gdpr.Permissions, currencyConverter *currencies.RateConverter) Exchange {
	e := new(exchange)

	e.adapterMap = newAdapterMap(client, cfg, infos)
	e.cache = cache
	e.cacheTime = time.Duration(cfg.CacheURL.ExpectedTimeMillis) * time.Millisecond
	e.me = metricsEngine
	e.gDPR = gDPR
	e.currencyConverter = currencyConverter
	e.UsersyncIfAmbiguous = cfg.GDPR.UsersyncIfAmbiguous
	e.defaultTTLs = cfg.CacheURL.DefaultTTLs
	return e
}

func (e *exchange) HoldAuction(ctx context.Context, bidRequest *openrtb.BidRequest, usersyncs IdFetcher, labels pbsmetrics.Labels, categoriesFetcher *stored_requests.CategoryFetcher) (*openrtb.BidResponse, error) {
	// Snapshot of resolved Bid request for debug if test request
	var resolvedRequest json.RawMessage
	if bidRequest.Test == 1 {
		if r, err := json.Marshal(bidRequest); err != nil {
			glog.Errorf("Error marshalling Bid request for debug: %v", err)
		} else {
			resolvedRequest = r
		}
	}

	// Slice of BidRequests, each a copy of the original cleaned to only contain Bidder data for the named Bidder
	blabels := make(map[openrtb_ext.BidderName]*pbsmetrics.AdapterLabels)
	cleanRequests, aliases, errs := CleanOpenRTBRequests(ctx, bidRequest, usersyncs, blabels, labels, e.gDPR, e.UsersyncIfAmbiguous)

	// List of bidders we have requests for.
	liveAdapters := make([]openrtb_ext.BidderName, len(cleanRequests))
	i := 0
	for a := range cleanRequests {
		liveAdapters[i] = a
		i++
	}
	// Randomize the list of adapters to make the Auction more fair
	RandomizeList(liveAdapters)
	// Process the request to check for targeting parameters.
	var targData *TargetData
	shouldCacheBids := false
	shouldCacheVAST := false
	var bidAdjustmentFactors map[string]float64
	var requestExt openrtb_ext.ExtRequest
	if len(bidRequest.Ext) > 0 {
		err := json.Unmarshal(bidRequest.Ext, &requestExt)
		if err != nil {
			return nil, fmt.Errorf("Error decoding Request.Ext : %s", err.Error())
		}
		bidAdjustmentFactors = requestExt.Prebid.BidAdjustmentFactors
		if requestExt.Prebid.Cache != nil {
			shouldCacheBids = requestExt.Prebid.Cache.Bids != nil
			shouldCacheVAST = requestExt.Prebid.Cache.VastXML != nil
		}

		if requestExt.Prebid.Targeting != nil {
			targData = &TargetData{
				PriceGranularity:  requestExt.Prebid.Targeting.PriceGranularity,
				IncludeWinners:    requestExt.Prebid.Targeting.IncludeWinners,
				IncludeBidderKeys: requestExt.Prebid.Targeting.IncludeBidderKeys,
			}
			if shouldCacheBids {
				targData.IncludeCacheBids = true
			}
			if shouldCacheVAST {
				targData.IncludeCacheVast = true
			}
		}
	}

	// If we need to cache Bids, then it will take some time to call prebid cache.
	// We should reduce the amount of time the bidders have, to compensate.
	auctionCtx, cancel := e.makeAuctionContext(ctx, shouldCacheBids)
	defer cancel()

	// Get Currency rates conversions for the Auction
	conversions := e.currencyConverter.Rates()

	adapterBids, adapterExtra := e.getAllBids(auctionCtx, cleanRequests, aliases, bidAdjustmentFactors, blabels, conversions)
	bidCategory, adapterBids, err := applyCategoryMapping(requestExt, adapterBids, *categoriesFetcher, targData)
	auc := NewAuction(adapterBids, len(bidRequest.Imp))
	if err != nil {
		return nil, fmt.Errorf("Error in category mapping : %s", err.Error())
	}

	if targData != nil && adapterBids != nil {
		auc.SetRoundedPrices(targData.PriceGranularity)
		cacheErrs := auc.doCache(ctx, e.cache, targData.IncludeCacheBids, targData.IncludeCacheVast, bidRequest, 60, &e.defaultTTLs, bidCategory)
		if len(cacheErrs) > 0 {
			errs = append(errs, cacheErrs...)
		}
		targData.SetTargeting(auc, bidRequest.App != nil, bidCategory)
	}
	// Build the response
	return e.buildBidResponse(ctx, liveAdapters, adapterBids, bidRequest, resolvedRequest, adapterExtra, errs)
}

func (e *exchange) makeAuctionContext(ctx context.Context, needsCache bool) (auctionCtx context.Context, cancel func()) {
	auctionCtx = ctx
	cancel = func() {}
	if needsCache {
		if deadline, ok := ctx.Deadline(); ok {
			auctionCtx, cancel = context.WithDeadline(ctx, deadline.Add(-e.cacheTime))
		}
	}
	return
}

// This piece sends all the requests to the Bidder adapters and gathers the results.
func (e *exchange) getAllBids(ctx context.Context, cleanRequests map[openrtb_ext.BidderName]*openrtb.BidRequest, aliases map[string]string, bidAdjustments map[string]float64, blabels map[openrtb_ext.BidderName]*pbsmetrics.AdapterLabels, conversions currencies.Conversions) (map[openrtb_ext.BidderName]*PBSOrtbSeatBid, map[openrtb_ext.BidderName]*SeatResponseExtra) {
	// Set up pointers to the Bid results
	adapterBids := make(map[openrtb_ext.BidderName]*PBSOrtbSeatBid, len(cleanRequests))
	adapterExtra := make(map[openrtb_ext.BidderName]*SeatResponseExtra, len(cleanRequests))
	chBids := make(chan *BidResponseWrapper, len(cleanRequests))

	for bidderName, req := range cleanRequests {
		// Here we actually call the adapters and collect the Bids.
		coreBidder := ResolveBidder(string(bidderName), aliases)
		bidderRunner := e.RecoverSafely(func(aName openrtb_ext.BidderName, coreBidder openrtb_ext.BidderName, request *openrtb.BidRequest, bidlabels *pbsmetrics.AdapterLabels, conversions currencies.Conversions) {
			// Passing in aName so a doesn't change out from under the go routine
			if bidlabels.Adapter == "" {
				glog.Errorf("Exchange: bidlables for %s (%s) missing adapter string", aName, coreBidder)
				bidlabels.Adapter = coreBidder
			}
			brw := new(BidResponseWrapper)
			brw.Bidder = aName
			// Defer basic metrics to insure we capture them after all the values have been set
			defer func() {
				e.me.RecordAdapterRequest(*bidlabels)
			}()
			start := time.Now()

			adjustmentFactor := 1.0
			if givenAdjustment, ok := bidAdjustments[string(aName)]; ok {
				adjustmentFactor = givenAdjustment
			}
			bids, err := e.adapterMap[coreBidder].RequestBid(ctx, request, aName, adjustmentFactor, conversions)

			// Add in time reporting
			elapsed := time.Since(start)
			brw.AdapterBids = bids
			// Structure to record extra tracking data generated during bidding
			ae := new(SeatResponseExtra)
			ae.ResponseTimeMillis = int(elapsed / time.Millisecond)
			// Timing statistics
			e.me.RecordAdapterTime(*bidlabels, time.Since(start))
			serr := ErrsToBidderErrors(err)
			bidlabels.AdapterBids = BidsToMetric(brw.AdapterBids)
			bidlabels.AdapterErrors = ErrorsToMetric(err)
			// Append any Bid validation errors to the error list
			ae.Errors = serr
			brw.AdapterExtra = ae
			if bids != nil {
				for _, bid := range bids.Bids {
					var cpm = float64(bid.Bid.Price * 1000)
					e.me.RecordAdapterPrice(*bidlabels, cpm)
					e.me.RecordAdapterBidReceived(*bidlabels, bid.BidType, bid.Bid.AdM != "")
				}
			}
			chBids <- brw
		}, chBids)
		go bidderRunner(bidderName, coreBidder, req, blabels[coreBidder], conversions)
	}
	// Wait for the bidders to do their thing
	for i := 0; i < len(cleanRequests); i++ {
		brw := <-chBids
		adapterBids[brw.Bidder] = brw.AdapterBids
		adapterExtra[brw.Bidder] = brw.AdapterExtra
	}

	return adapterBids, adapterExtra
}

func (e *exchange) RecoverSafely(inner func(openrtb_ext.BidderName, openrtb_ext.BidderName, *openrtb.BidRequest, *pbsmetrics.AdapterLabels, currencies.Conversions), chBids chan *BidResponseWrapper) func(openrtb_ext.BidderName, openrtb_ext.BidderName, *openrtb.BidRequest, *pbsmetrics.AdapterLabels, currencies.Conversions) {
	return func(aName openrtb_ext.BidderName, coreBidder openrtb_ext.BidderName, request *openrtb.BidRequest, bidlabels *pbsmetrics.AdapterLabels, conversions currencies.Conversions) {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("OpenRTB Auction recovered panic from Bidder %s: %v. Stack trace is: %v", coreBidder, r, string(debug.Stack()))
				e.me.RecordAdapterPanic(*bidlabels)
				// Let the master request know that there is no data here
				brw := new(BidResponseWrapper)
				brw.AdapterExtra = new(SeatResponseExtra)
				chBids <- brw
			}
		}()
		inner(aName, coreBidder, request, bidlabels, conversions)
	}
}

func BidsToMetric(bids *PBSOrtbSeatBid) pbsmetrics.AdapterBid {
	if bids == nil || len(bids.Bids) == 0 {
		return pbsmetrics.AdapterBidNone
	}
	return pbsmetrics.AdapterBidPresent
}

func ErrorsToMetric(errs []error) map[pbsmetrics.AdapterError]struct{} {
	if len(errs) == 0 {
		return nil
	}
	ret := make(map[pbsmetrics.AdapterError]struct{}, len(errs))
	var s struct{}
	for _, err := range errs {
		switch errortypes.DecodeError(err) {
		case errortypes.TimeoutCode:
			ret[pbsmetrics.AdapterErrorTimeout] = s
		case errortypes.BadInputCode:
			ret[pbsmetrics.AdapterErrorBadInput] = s
		case errortypes.BadServerResponseCode:
			ret[pbsmetrics.AdapterErrorBadServerResponse] = s
		case errortypes.FailedToRequestBidsCode:
			ret[pbsmetrics.AdapterErrorFailedToRequestBids] = s
		default:
			ret[pbsmetrics.AdapterErrorUnknown] = s
		}
	}
	return ret
}

func ErrsToBidderErrors(errs []error) []openrtb_ext.ExtBidderError {
	serr := make([]openrtb_ext.ExtBidderError, len(errs))
	for i := 0; i < len(errs); i++ {
		serr[i].Code = errortypes.DecodeError(errs[i])
		serr[i].Message = errs[i].Error()
	}
	return serr
}

// This piece takes all the Bids supplied by the adapters and crafts an openRTB response to send back to the requester
func (e *exchange) buildBidResponse(ctx context.Context, liveAdapters []openrtb_ext.BidderName, adapterBids map[openrtb_ext.BidderName]*PBSOrtbSeatBid, bidRequest *openrtb.BidRequest, resolvedRequest json.RawMessage, adapterExtra map[openrtb_ext.BidderName]*SeatResponseExtra, errList []error) (*openrtb.BidResponse, error) {
	bidResponse := new(openrtb.BidResponse)

	bidResponse.ID = bidRequest.ID
	if len(liveAdapters) == 0 {
		// signal "Invalid Request" if no valid bidders.
		bidResponse.NBR = openrtb.NoBidReasonCode.Ptr(openrtb.NoBidReasonCodeInvalidRequest)
	}

	// Create the SeatBids. We use a zero sized slice so that we can append non-zero seat Bids, and not include seatBid
	// objects for seatBids without any Bids. Preallocate the max possible size to avoid reallocating the array as we go.
	seatBids := make([]openrtb.SeatBid, 0, len(liveAdapters))
	for _, a := range liveAdapters {
		//while processing every single bib, do we need to handle categories here?
		if adapterBids[a] != nil && len(adapterBids[a].Bids) > 0 {
			sb := e.makeSeatBid(adapterBids[a], a, adapterExtra)
			seatBids = append(seatBids, *sb)
		}
	}

	bidResponse.SeatBid = seatBids

	bidResponseExt := e.makeExtBidResponse(adapterBids, adapterExtra, bidRequest, resolvedRequest, errList)
	buffer := &bytes.Buffer{}
	enc := json.NewEncoder(buffer)
	enc.SetEscapeHTML(false)
	err := enc.Encode(bidResponseExt)
	bidResponse.Ext = buffer.Bytes()

	return bidResponse, err
}

func applyCategoryMapping(requestExt openrtb_ext.ExtRequest, seatBids map[openrtb_ext.BidderName]*PBSOrtbSeatBid, categoriesFetcher stored_requests.CategoryFetcher, targData *TargetData) (map[string]string, map[openrtb_ext.BidderName]*PBSOrtbSeatBid, error) {
	res := make(map[string]string)

	type bidDedupe struct {
		bidderName openrtb_ext.BidderName
		bidIndex   int
		bidID      string
	}

	dedupe := make(map[string]bidDedupe)

	//If includebrandcategory is present in Ext then CE feature is on.
	if requestExt.Prebid.Targeting == nil {
		return res, seatBids, nil
	}
	brandCatExt := requestExt.Prebid.Targeting.IncludeBrandCategory

	//If Ext.prebid.targeting.includebrandcategory is present in Ext then competitive exclusion feature is on.
	if brandCatExt == (openrtb_ext.ExtIncludeBrandCategory{}) {
		return res, seatBids, nil //if not present continue the existing processing without CE.
	}

	//if Ext.prebid.targeting.includebrandcategory present but primaryadserver/publisher not present then error out the request right away.
	primaryAdServer, err := getPrimaryAdServer(brandCatExt.PrimaryAdServer) //1-Freewheel 2-DFP
	if err != nil {
		return res, seatBids, err
	}

	publisher := brandCatExt.Publisher

	seatBidsToRemove := make([]openrtb_ext.BidderName, 0)

	for bidderName, seatBid := range seatBids {
		bidsToRemove := make([]int, 0)
		for bidInd := range seatBid.Bids {
			bid := seatBid.Bids[bidInd]
			var duration int
			var category string
			var pb string

			if bid.BidVideo != nil {
				duration = bid.BidVideo.Duration
				category = bid.BidVideo.PrimaryCategory
			}

			if category == "" {
				bidIabCat := bid.Bid.Cat
				if len(bidIabCat) != 1 {
					//TODO: add metrics
					//on receiving Bids from adapters if no unique IAB category is returned  or if no ad server category is returned discard the Bid
					bidsToRemove = append(bidsToRemove, bidInd)
					continue
				} else {
					//if unique IAB category is present then translate it to the adserver category based on mapping file
					category, err = categoriesFetcher.FetchCategories(primaryAdServer, publisher, bidIabCat[0])
					if err != nil || category == "" {
						//TODO: add metrics
						//if mapping required but no mapping file is found then discard the Bid
						bidsToRemove = append(bidsToRemove, bidInd)
						continue
					}
				}
			}

			// TODO: consider should we remove Bids with zero duration here?

			pb, _ = GetCpmStringValue(bid.Bid.Price, targData.PriceGranularity)

			newDur := duration
			if len(requestExt.Prebid.Targeting.DurationRangeSec) > 0 {
				durationRange := requestExt.Prebid.Targeting.DurationRangeSec
				sort.Ints(durationRange)
				//if the Bid is above the range of the listed durations (and outside the buffer), reject the Bid
				if duration > durationRange[len(durationRange)-1] {
					bidsToRemove = append(bidsToRemove, bidInd)
					continue
				}
				for _, dur := range durationRange {
					if duration <= dur {
						newDur = dur
						break
					}
				}
			}

			categoryDuration := fmt.Sprintf("%s_%s_%ds", pb, category, newDur)

			if dupe, ok := dedupe[categoryDuration]; ok {
				// 50% chance for either Bid with duplicate categoryDuration values to be kept
				if rand.Intn(100) < 50 {
					if dupe.bidderName == bidderName {
						// An older Bid from the current Bidder
						bidsToRemove = append(bidsToRemove, dupe.bidIndex)
					} else {
						// An older Bid from a different seatBid we've already finished with
						oldSeatBid := (seatBids)[dupe.bidderName]
						if len(oldSeatBid.Bids) == 1 {
							seatBidsToRemove = append(seatBidsToRemove, bidderName)
						} else {
							oldSeatBid.Bids = append(oldSeatBid.Bids[:dupe.bidIndex], oldSeatBid.Bids[dupe.bidIndex+1:]...)
						}
					}
					delete(res, dupe.bidID)
				} else {
					// Remove this Bid
					bidsToRemove = append(bidsToRemove, bidInd)
					continue
				}
			}
			res[bid.Bid.ID] = categoryDuration
			dedupe[categoryDuration] = bidDedupe{bidderName: bidderName, bidIndex: bidInd, bidID: bid.Bid.ID}
		}

		if len(bidsToRemove) > 0 {
			sort.Ints(bidsToRemove)
			if len(bidsToRemove) == len(seatBid.Bids) {
				//if all Bids are invalid - remove entire seat Bid
				seatBidsToRemove = append(seatBidsToRemove, bidderName)
			} else {
				bids := seatBid.Bids
				for i := len(bidsToRemove) - 1; i >= 0; i-- {
					remInd := bidsToRemove[i]
					bids = append(bids[:remInd], bids[remInd+1:]...)
				}
				seatBid.Bids = bids
			}
		}

	}
	if len(seatBidsToRemove) > 0 {
		if len(seatBidsToRemove) == len(seatBids) {
			//delete all seat Bids
			seatBids = nil
		} else {
			for _, seatBidInd := range seatBidsToRemove {
				delete(seatBids, seatBidInd)
			}

		}
	}

	return res, seatBids, nil
}

func getPrimaryAdServer(adServerId int) (string, error) {
	switch adServerId {
	case 1:
		return "freewheel", nil
	case 2:
		return "dfp", nil
	default:
		return "", fmt.Errorf("Primary ad server %d not recognized", adServerId)
	}
}

// Extract all the data from the SeatBids and build the ExtBidResponse
func (e *exchange) makeExtBidResponse(adapterBids map[openrtb_ext.BidderName]*PBSOrtbSeatBid, adapterExtra map[openrtb_ext.BidderName]*SeatResponseExtra, req *openrtb.BidRequest, resolvedRequest json.RawMessage, errList []error) *openrtb_ext.ExtBidResponse {
	bidResponseExt := &openrtb_ext.ExtBidResponse{
		Errors:               make(map[openrtb_ext.BidderName][]openrtb_ext.ExtBidderError, len(adapterBids)),
		ResponseTimeMillis:   make(map[openrtb_ext.BidderName]int, len(adapterBids)),
		RequestTimeoutMillis: req.TMax,
	}
	if req.Test == 1 {
		bidResponseExt.Debug = &openrtb_ext.ExtResponseDebug{
			HttpCalls: make(map[openrtb_ext.BidderName][]*openrtb_ext.ExtHttpCall),
		}
		if err := json.Unmarshal(resolvedRequest, &bidResponseExt.Debug.ResolvedRequest); err != nil {
			glog.Errorf("Error unmarshalling Bid request snapshot: %v", err)
		}
	}

	for a, b := range adapterBids {
		if b != nil {
			if req.Test == 1 {
				// Fill debug info
				bidResponseExt.Debug.HttpCalls[a] = b.HTTPCalls
			}
		}
		// Only make an entry for Bidder errors if the Bidder reported any.
		if len(adapterExtra[a].Errors) > 0 {
			bidResponseExt.Errors[a] = adapterExtra[a].Errors
		}
		if len(errList) > 0 {
			bidResponseExt.Errors["prebid"] = ErrsToBidderErrors(errList)
		}
		bidResponseExt.ResponseTimeMillis[a] = adapterExtra[a].ResponseTimeMillis
		// Defering the filling of bidResponseExt.Usersync[a] until later

	}
	return bidResponseExt
}

// Return an openrtb seatBid for a Bidder
// BuildBidResponse is responsible for ensuring nil Bid seatbids are not included
func (e *exchange) makeSeatBid(adapterBid *PBSOrtbSeatBid, adapter openrtb_ext.BidderName, adapterExtra map[openrtb_ext.BidderName]*SeatResponseExtra) *openrtb.SeatBid {
	seatBid := new(openrtb.SeatBid)
	seatBid.Seat = adapter.String()
	// Prebid cannot support roadblocking
	seatBid.Group = 0

	if len(adapterBid.Ext) > 0 {
		sbExt := ExtSeatBid{
			Bidder: adapterBid.Ext,
		}

		ext, err := json.Marshal(sbExt)
		if err != nil {
			extError := openrtb_ext.ExtBidderError{
				Code:    errortypes.DecodeError(err),
				Message: fmt.Sprintf("Error writing SeatBid.Ext: %s", err.Error()),
			}
			adapterExtra[adapter].Errors = append(adapterExtra[adapter].Errors, extError)
		}
		seatBid.Ext = ext
	}

	var errList []error
	seatBid.Bid, errList = e.makeBid(adapterBid.Bids, adapter)
	if len(errList) > 0 {
		adapterExtra[adapter].Errors = append(adapterExtra[adapter].Errors, ErrsToBidderErrors(errList)...)
	}

	return seatBid
}

// Create the Bid array inside of SeatBid
func (e *exchange) makeBid(Bids []*PBSOrtbBid, adapter openrtb_ext.BidderName) ([]openrtb.Bid, []error) {
	bids := make([]openrtb.Bid, 0, len(Bids))
	errList := make([]error, 0, 1)
	for _, thisBid := range Bids {
		bidExt := &openrtb_ext.ExtBid{
			Bidder: thisBid.Bid.Ext,
			Prebid: &openrtb_ext.ExtBidPrebid{
				Targeting: thisBid.BidTargets,
				Type:      thisBid.BidType,
				Video:     thisBid.BidVideo,
			},
		}

		ext, err := json.Marshal(bidExt)
		if err != nil {
			errList = append(errList, err)
		} else {
			bids = append(bids, *thisBid.Bid)
			bids[len(bids)-1].Ext = ext
		}
	}
	return bids, errList
}
