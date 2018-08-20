package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/analytics"
	analyticsConf "github.com/prebid/prebid-server/analytics/config"
	"github.com/prebid/prebid-server/cache/dummycache"
	"github.com/prebid/prebid-server/cache/filecache"
	"github.com/prebid/prebid-server/cache/postgrescache"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/endpoints"
	infoEndpoints "github.com/prebid/prebid-server/endpoints/info"
	"github.com/prebid/prebid-server/endpoints/openrtb2"
	"github.com/prebid/prebid-server/exchange"
	"github.com/prebid/prebid-server/gdpr"
	"github.com/prebid/prebid-server/openrtb_ext"
	"github.com/prebid/prebid-server/pbs"
	metricsConf "github.com/prebid/prebid-server/pbsmetrics/config"
	pbc "github.com/prebid/prebid-server/prebid_cache_client"
	"github.com/prebid/prebid-server/stored_requests"
	storedRequestsConf "github.com/prebid/prebid-server/stored_requests/config"

	"github.com/golang/glog"
	"github.com/julienschmidt/httprouter"
	_ "github.com/lib/pq"
	"github.com/rs/cors"
)

const schemaDirectory = "./static/bidder-params"

// NewJsonDirectoryServer is used to serve .json files from a directory as a single blob. For example,
// given a directory containing the files "a.json" and "b.json", this returns a Handle which serves JSON like:
//
// {
//   "a": { ... content from the file a.json ... },
//   "b": { ... content from the file b.json ... }
// }
//
// This function stores the file contents in memory, and should not be used on large directories.
// If the root directory, or any of the files in it, cannot be read, then the program will exit.
func NewJsonDirectoryServer(validator openrtb_ext.BidderParamValidator) httprouter.Handle {
	// Slurp the files into memory first, since they're small and it minimizes request latency.
	files, err := ioutil.ReadDir(schemaDirectory)
	if err != nil {
		glog.Fatalf("Failed to read directory %s: %v", schemaDirectory, err)
	}

	data := make(map[string]json.RawMessage, len(files))
	for _, file := range files {
		bidder := strings.TrimSuffix(file.Name(), ".json")
		bidderName, isValid := adapters.LookupBidder(bidder)
		if !isValid {
			glog.Fatalf("Schema exists for an unknown bidder: %s", bidder)
		}
		data[bidder] = json.RawMessage(validator.Schema(bidderName))
	}
	response, err := json.Marshal(data)
	if err != nil {
		glog.Fatalf("Failed to marshal bidder param JSON-schema: %v", err)
	}

	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		w.Header().Add("Content-Type", "application/json")
		w.Write(response)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	http.ServeFile(w, r, "static/index.html")
}

type NoCache struct {
	Handler http.Handler
}

func (m NoCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Add("Pragma", "no-cache")
	w.Header().Add("Expires", "0")
	m.Handler.ServeHTTP(w, r)
}

func loadDataCache(cfg *config.Configuration, db *sql.DB) (err error) {
	switch cfg.DataCache.Type {
	case "dummy":
		dataCache, err = dummycache.New()
		if err != nil {
			glog.Fatalf("Dummy cache not configured: %s", err.Error())
		}

	case "postgres":
		if db == nil {
			return fmt.Errorf("Nil db cannot connect to postgres. Did you forget to set the config.stored_requests.postgres values?")
		}
		dataCache = postgrescache.New(db, postgrescache.CacheConfig{
			Size: cfg.DataCache.CacheSize,
			TTL:  cfg.DataCache.TTLSeconds,
		})
		return nil
	case "filecache":
		dataCache, err = filecache.New(cfg.DataCache.Filename)
		if err != nil {
			return fmt.Errorf("FileCache Error: %s", err.Error())
		}

	default:
		return fmt.Errorf("Unknown datacache.type: %s", cfg.DataCache.Type)
	}
	return nil
}

type Router struct {
	*httprouter.Router
	Analytics       analytics.PBSAnalyticsModule
	Fetcher         stored_requests.Fetcher
	AmpFetcher      stored_requests.Fetcher
	MetricsEngine   *metricsConf.DetailedMetricsEngine
	ParamsValidator openrtb_ext.BidderParamValidator
	Shutdown        func()
}

func New(cfg *config.Configuration) (r *Router, err error) {
	r = &Router{
		Router: httprouter.New(),
	}
	fetcher, ampFetcher, db, shutdown := storedRequestsConf.NewStoredRequests(&cfg.StoredRequests, cfg.HttpClient, r.Router)
	r.Shutdown = shutdown
	r.Fetcher = fetcher
	r.AmpFetcher = ampFetcher
	if err := loadDataCache(cfg, db); err != nil {
		return nil, fmt.Errorf("Prebid Server could not load data cache: %v", err)
	}

	// Hack because of how legacy handles districtm
	bidderList := adapters.BidderList()
	bidderList = append(bidderList, openrtb_ext.BidderName("districtm"))

	r.MetricsEngine = metricsConf.NewMetricsEngine(cfg, bidderList)

	r.ParamsValidator, err = openrtb_ext.NewBidderParamsValidator(schemaDirectory)
	if err != nil {
		glog.Fatalf("Failed to create the bidder params validator. %v", err)
	}

	r.Analytics = analyticsConf.NewPBSAnalytics(&cfg.Analytics)

	exchanges = adapters.LegacyExchangeMap()
	theExchange := exchange.NewExchange(pbc.NewClient(&cfg.CacheURL), cfg, r.MetricsEngine)

	openrtbEndpoint, err := openrtb2.NewEndpoint(theExchange, r.ParamsValidator, fetcher, cfg, r.MetricsEngine, r.Analytics)
	if err != nil {
		glog.Fatalf("Failed to create the openrtb endpoint handler. %v", err)
	}

	ampEndpoint, err := openrtb2.NewAmpEndpoint(theExchange, r.ParamsValidator, ampFetcher, cfg, r.MetricsEngine, r.Analytics)
	if err != nil {
		glog.Fatalf("Failed to create the amp endpoint handler. %v", err)
	}

	syncers := adapters.Syncers()
	gdprPerms := gdpr.NewPermissions(context.Background(), cfg.GDPR, adapters.GDPRAwareSyncerIDs(syncers), cfg.HttpClient)

	r.POST("/auction", (&auctionDeps{cfg, syncers, gdprPerms, r.MetricsEngine}).auction)
	r.POST("/openrtb2/auction", openrtbEndpoint)
	r.GET("/openrtb2/amp", ampEndpoint)
	r.GET("/info/bidders", infoEndpoints.NewBiddersEndpoint())
	r.GET("/info/bidders/:bidderName", infoEndpoints.NewBidderDetailsEndpoint())
	r.GET("/bidders/params", NewJsonDirectoryServer(r.ParamsValidator))
	r.POST("/cookie_sync", endpoints.NewCookieSyncEndpoint(syncers, cfg, gdprPerms, r.MetricsEngine, r.Analytics))
	r.GET("/status", endpoints.NewStatusEndpoint(cfg.StatusResponse))
	r.GET("/", serveIndex)
	r.ServeFiles("/static/*filepath", http.Dir("static"))
	userSyncDeps := &pbs.UserSyncDeps{
		HostCookieConfig: &(cfg.HostCookie),
		ExternalUrl:      cfg.ExternalURL,
		RecaptchaSecret:  cfg.RecaptchaSecret,
		MetricsEngine:    r.MetricsEngine,
		PBSAnalytics:     r.Analytics,
	}
	r.GET("/setuid", endpoints.NewSetUIDEndpoint(cfg.HostCookie, gdprPerms, r.Analytics, r.MetricsEngine))
	r.POST("/optout", userSyncDeps.OptOut)
	r.GET("/optout", userSyncDeps.OptOut)
	return r, nil
}

// Fixes #648
//
// These CORS options pose a security risk... but it's a calculated one.
// People _must_ call us with "withCredentials" set to "true" because that's how we use the cookie sync info.
// We also must allow all origins because every site on the internet _could_ call us.
//
// This is an inherent security risk. However, PBS doesn't use cookies for authorization--just identification.
// We only store the User's ID for each Bidder, and each Bidder has already exposed a public cookie sync endpoint
// which returns that data anyway.
//
// For more info, see:
//
// - https://github.com/rs/cors/issues/55
// - https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS/Errors/CORSNotSupportingCredentials
// - https://portswigger.net/blog/exploiting-cors-misconfigurations-for-bitcoins-and-bounties
func SupportCORS(handler http.Handler) http.Handler {
	c := cors.New(cors.Options{
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		AllowedHeaders: []string{"Origin", "X-Requested-With", "Content-Type", "Accept"}})
	return c.Handler(handler)
}
