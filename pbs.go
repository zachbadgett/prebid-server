package main

import (
	"flag"
	"math/rand"
	"path/filepath"
	"time"

	"github.com/prebid/prebid-server/adapters"
	_ "github.com/prebid/prebid-server/adapters/all"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/openrtb_ext"
	metricsConf "github.com/prebid/prebid-server/pbsmetrics/config"
	pbc "github.com/prebid/prebid-server/prebid_cache_client"
	"github.com/prebid/prebid-server/router"
	"github.com/prebid/prebid-server/server"

	"github.com/golang/glog"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
)

// Holds binary revision string
// Set manually at build time using:
//    go build -ldflags "-X main.Rev=`git rev-parse --short HEAD`"
// Populated automatically at build / release time via .travis.yml
//   `gox -os="linux" -arch="386" -output="{{.Dir}}_{{.OS}}_{{.Arch}}" -ldflags "-X main.Rev=`git rev-parse --short HEAD`" -verbose ./...;`
// See issue #559
var Rev string

func init() {
	rand.Seed(time.Now().UnixNano())
	flag.Parse() // read glog settings from cmd line
}

func main() {
	v := viper.New()
	config.SetupViper(v, "pbs", adapters.BidderList())
	cfg, err := config.New(v)
	if err != nil {
		glog.Fatalf("Configuration could not be loaded or did not pass validation: %v", err)
	}

	if err := serve(Rev, cfg); err != nil {
		glog.Errorf("prebid-server failed: %v", err)
	}
}

func serve(revision string, cfg *config.Configuration) error {
	d, err := filepath.Abs("./static/bidder-info")
	if err != nil {
		return err
	}
	adapters.Configure(cfg, d)
	r, err := router.New(cfg)
	if err != nil {
		return err
	}
	defer r.Shutdown()
	bidderList := adapters.BidderList()
	bidderList = append(bidderList, openrtb_ext.BidderName("districtm"))
	metricsEngine := metricsConf.NewMetricsEngine(cfg, bidderList)
	pbc.InitPrebidCache(cfg.CacheURL.GetBaseURL())
	corsRouter := router.SupportCORS(r)
	server.Listen(cfg, router.NoCache{Handler: corsRouter}, router.Admin(revision), metricsEngine)
	return nil
}
