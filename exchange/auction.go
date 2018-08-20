package exchange

import (
	"context"
	"encoding/json"

	"github.com/golang/glog"
	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/openrtb_ext"
	"github.com/prebid/prebid-server/prebid_cache_client"
)

func newAuction(seatBids map[openrtb_ext.BidderName]*adapters.SeatBid, numImps int) *auction {
	winningBids := make(map[string]*adapters.Bid, numImps)
	winningBidsByBidder := make(map[string]map[openrtb_ext.BidderName]*adapters.Bid, numImps)

	for bidderName, seatBid := range seatBids {
		if seatBid != nil {
			for _, bid := range seatBid.Bids {
				cpm := bid.Bid.Price
				wbid, ok := winningBids[bid.Bid.ImpID]
				if !ok || cpm > wbid.Bid.Price {
					winningBids[bid.Bid.ImpID] = bid
				}
				if bidMap, ok := winningBidsByBidder[bid.Bid.ImpID]; ok {
					bestSoFar, ok := bidMap[bidderName]
					if !ok || cpm > bestSoFar.Bid.Price {
						bidMap[bidderName] = bid
					}
				} else {
					winningBidsByBidder[bid.Bid.ImpID] = make(map[openrtb_ext.BidderName]*adapters.Bid)
					winningBidsByBidder[bid.Bid.ImpID][bidderName] = bid
				}
			}
		}
	}

	return &auction{
		winningBids:         winningBids,
		winningBidsByBidder: winningBidsByBidder,
	}
}

func (a *auction) setRoundedPrices(priceGranularity openrtb_ext.PriceGranularity) {
	roundedPrices := make(map[*adapters.Bid]string, 5*len(a.winningBids))
	for _, topBidsPerImp := range a.winningBidsByBidder {
		for _, topBidPerBidder := range topBidsPerImp {
			roundedPrice, err := GetCpmStringValue(topBidPerBidder.Bid.Price, priceGranularity)
			if err != nil {
				glog.Errorf(`Error rounding price according to granularity. This shouldn't happen unless /openrtb2 input validation is buggy. Granularity was "%v".`, priceGranularity)
			}
			roundedPrices[topBidPerBidder] = roundedPrice
		}
	}
	a.roundedPrices = roundedPrices
}

func (a *auction) doCache(ctx context.Context, cache prebid_cache_client.Client, bids bool, vast bool) {
	if !bids && !vast {
		return
	}

	expectNumBids := valOrZero(bids, len(a.roundedPrices))
	expectNumVast := valOrZero(vast, len(a.roundedPrices))
	bidIndices := make(map[int]*openrtb.Bid, expectNumBids)
	vastIndices := make(map[int]*openrtb.Bid, expectNumVast)
	toCache := make([]prebid_cache_client.Cacheable, 0, expectNumBids+expectNumVast)

	for _, topBidsPerImp := range a.winningBidsByBidder {
		for _, topBidPerBidder := range topBidsPerImp {
			if bids {
				if jsonBytes, err := json.Marshal(topBidPerBidder.Bid); err == nil {
					toCache = append(toCache, prebid_cache_client.Cacheable{
						Type: prebid_cache_client.TypeJSON,
						Data: jsonBytes,
					})
					bidIndices[len(toCache)-1] = topBidPerBidder.Bid
				}
			}
			if vast && topBidPerBidder.BidType == openrtb_ext.BidTypeVideo {
				vast := makeVAST(topBidPerBidder.Bid)
				if jsonBytes, err := json.Marshal(vast); err == nil {
					toCache = append(toCache, prebid_cache_client.Cacheable{
						Type: prebid_cache_client.TypeXML,
						Data: jsonBytes,
					})
					vastIndices[len(toCache)-1] = topBidPerBidder.Bid
				}
			}
		}
	}

	ids := cache.PutJson(ctx, toCache)

	if bids {
		a.cacheIds = make(map[*openrtb.Bid]string, len(bidIndices))
		for index, bid := range bidIndices {
			if ids[index] != "" {
				a.cacheIds[bid] = ids[index]
			}
		}
	}
	if vast {
		a.vastCacheIds = make(map[*openrtb.Bid]string, len(vastIndices))
		for index, bid := range vastIndices {
			if ids[index] != "" {
				a.vastCacheIds[bid] = ids[index]
			}
		}
	}
}

// makeVAST returns some VAST XML for the given bid. If AdM is defined,
// it takes precedence. Otherwise the Nurl will be wrapped in a redirect tag.
func makeVAST(bid *openrtb.Bid) string {
	if bid.AdM == "" {
		return `<VAST version="3.0"><Ad><Wrapper>` +
			`<AdSystem>prebid.org wrapper</AdSystem>` +
			`<VASTAdTagURI><![CDATA[` + bid.NURL + `]]></VASTAdTagURI>` +
			`<Impression></Impression><Creatives></Creatives>` +
			`</Wrapper></Ad></VAST>`
	}
	return bid.AdM
}

func valOrZero(useVal bool, val int) int {
	if useVal {
		return val
	}
	return 0
}

func maybeMake(shouldMake bool, capacity int) []prebid_cache_client.Cacheable {
	if shouldMake {
		return make([]prebid_cache_client.Cacheable, 0, capacity)
	}
	return nil
}

type auction struct {
	// winningBids is a map from imp.id to the highest overall CPM bid in that imp.
	winningBids map[string]*adapters.Bid
	// winningBidsByBidder stores the highest bid on each imp by each bidder.
	winningBidsByBidder map[string]map[openrtb_ext.BidderName]*adapters.Bid
	// roundedPrices stores the price strings rounded for each bid according to the price granularity.
	roundedPrices map[*adapters.Bid]string
	// cacheIds stores the UUIDs from Prebid Cache for fetching the full bid JSON.
	cacheIds map[*openrtb.Bid]string
	// vastCacheIds stores UUIDS from Prebid cache for fetching the VAST markup to video bids.
	vastCacheIds map[*openrtb.Bid]string
}
