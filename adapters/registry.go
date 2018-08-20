package adapters

import (
	"sync/atomic"

	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/openrtb_ext"
	"github.com/prebid/prebid-server/usersync"
)

type bidderInit func(cfg *config.Configuration, info BidderInfo) Bidder
type adapterInit func(cfg *config.Configuration) Adapter
type syncerInit func(cfg *config.Configuration) usersync.Usersyncer

type registryEntry struct {
	bidderName  openrtb_ext.BidderName
	bidderInit  bidderInit
	adapterName string
	adapterInit adapterInit
	syncerInit  syncerInit
}

var (
	inits      = map[openrtb_ext.BidderName]*registryEntry{}
	bidders    = map[openrtb_ext.BidderName]Bidder{}
	adapters   = map[string]Adapter{}
	syncers    = map[openrtb_ext.BidderName]usersync.Usersyncer{}
	requesters = map[openrtb_ext.BidderName]Requester{}
	bidderMap  = map[string]openrtb_ext.BidderName{}
	infos      = BidderInfos{}
)

func Register(bidderName openrtb_ext.BidderName, opts ...registryOption) {
	e := &registryEntry{
		bidderName: bidderName,
	}
	for _, fn := range opts {
		fn(e)
	}
	inits[bidderName] = e
}

func LegacyExchangeMap() map[string]Adapter {
	return adapters
}

func AdapterMap() map[openrtb_ext.BidderName]Requester {
	return requesters
}

func LookupBidder(name string) (openrtb_ext.BidderName, bool) {
	v, found := bidderMap[name]
	return v, found
}

// BidderList returns the values of the BidderMap
func BidderList() []openrtb_ext.BidderName {
	bidders := make([]openrtb_ext.BidderName, 0)
	for _, value := range inits {
		bidders = append(bidders, value.bidderName)
	}
	return bidders
}

func Syncers() map[openrtb_ext.BidderName]usersync.Usersyncer {
	return syncers
}

func BidderMap() map[openrtb_ext.BidderName]Bidder {
	return bidders
}

func Infos() BidderInfos {
	return infos
}

var configured uint32

func Configure(cfg *config.Configuration, infoDir string) {
	if atomic.CompareAndSwapUint32(&configured, 0, 1) {
		infos = ParseBidderInfos(infoDir, BidderList())
		for _, entry := range inits {
			name := entry.bidderName
			if a := entry.adapterInit; a != nil {
				adapters[entry.adapterName] = a(cfg)
				requesters[entry.bidderName] = LegacyAdapter(adapters[entry.adapterName]).(Requester)
			}
			if b := entry.bidderInit; b != nil {
				bidders[entry.bidderName] = b(cfg, infos[string(entry.bidderName)])
				// Allow bidders to have their own RequestBid method
				if requester, ok := bidders[entry.bidderName].(Requester); !ok {
					requesters[entry.bidderName] = requester
				} else {
					// todo: pass http client through cfg or through configure
					requesters[entry.bidderName] = NewBidRequester(bidders[entry.bidderName], cfg.HttpClient)
				}
			}
			bidderMap[string(entry.bidderName)] = entry.bidderName
			if s := entry.syncerInit; s != nil {
				syncers[name] = s(cfg)
			}
		}
	}
}
