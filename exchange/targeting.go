package exchange

import (
	"strconv"

	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/openrtb_ext"
)

const maxKeyLength = 20

// TargetData tracks information about the winning Bid in each Imp.
//
// All functions on this struct are nil-safe. If the TargetData struct is nil, then they behave
// like they would if no targeting information is needed.
//
// All functions on this struct are all nil-safe.
// If the value is nil, then no targeting data will be tracked.
type TargetData struct {
	PriceGranularity  openrtb_ext.PriceGranularity
	IncludeWinners    bool
	IncludeBidderKeys bool
	IncludeCacheBids  bool
	IncludeCacheVast  bool
}

// SetTargeting writes all the targeting params into the Bids.
// If any errors occur when setting the targeting params for a particular Bid, then that Bid will be ejected from the Auction.
//
// The one exception is the `hb_cache_id` key. Since our APIs explicitly document cache keys to be on a "best effort" basis,
// it's ok if those stay in the Auction. For now, this method implements a very naive cache strategy.
// In the future, we should implement a more clever retry & backoff strategy to balance the success rate & performance.
func (targData *TargetData) SetTargeting(auc *Auction, isApp bool, categoryMapping map[string]string) {
	for impId, topBidsPerImp := range auc.winningBidsByBidder {
		overallWinner := auc.winningBids[impId]
		for bidderName, topBidPerBidder := range topBidsPerImp {
			isOverallWinner := overallWinner == topBidPerBidder

			targets := make(map[string]string, 10)
			if cpm, ok := auc.roundedPrices[topBidPerBidder]; ok {
				targData.addKeys(targets, openrtb_ext.HbpbConstantKey, cpm, bidderName, isOverallWinner)
			}
			targData.addKeys(targets, openrtb_ext.HbBidderConstantKey, string(bidderName), bidderName, isOverallWinner)
			if hbSize := makeHbSize(topBidPerBidder.Bid); hbSize != "" {
				targData.addKeys(targets, openrtb_ext.HbSizeConstantKey, hbSize, bidderName, isOverallWinner)
			}
			if cacheID, ok := auc.cacheIds[topBidPerBidder.Bid]; ok {
				targData.addKeys(targets, openrtb_ext.HbCacheKey, cacheID, bidderName, isOverallWinner)
			}
			if vastID, ok := auc.vastCacheIds[topBidPerBidder.Bid]; ok {
				targData.addKeys(targets, openrtb_ext.HbVastCacheKey, vastID, bidderName, isOverallWinner)
			}
			if deal := topBidPerBidder.Bid.DealID; len(deal) > 0 {
				targData.addKeys(targets, openrtb_ext.HbDealIdConstantKey, deal, bidderName, isOverallWinner)
			}

			if isApp {
				targData.addKeys(targets, openrtb_ext.HbEnvKey, openrtb_ext.HbEnvKeyApp, bidderName, isOverallWinner)
			}
			if len(categoryMapping) > 0 {
				targData.addKeys(targets, openrtb_ext.HbCategoryDurationKey, categoryMapping[topBidPerBidder.Bid.ID], bidderName, isOverallWinner)
			}

			topBidPerBidder.BidTargets = targets
		}
	}
}

func (targData *TargetData) addKeys(keys map[string]string, key openrtb_ext.TargetingKey, value string, bidderName openrtb_ext.BidderName, overallWinner bool) {
	if targData.IncludeBidderKeys {
		keys[key.BidderKey(bidderName, maxKeyLength)] = value
	}
	if targData.IncludeWinners && overallWinner {
		keys[string(key)] = value
	}
}

func makeHbSize(bid *openrtb.Bid) string {
	if bid.W != 0 && bid.H != 0 {
		return strconv.FormatUint(bid.W, 10) + "x" + strconv.FormatUint(bid.H, 10)
	}
	return ""
}
