package beachfront

import (
	"fmt"

	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/usersync"
)

func NewBeachfrontSyncer(cfg *config.Configuration) usersync.Usersyncer {
	usersyncURL := cfg.Adapters[string(BidderBeachfront)].UserSyncURL
	platformID := cfg.Adapters[string(BidderBeachfront)].PlatformID
	url := fmt.Sprintf("%s%s", usersyncURL, platformID)
	return adapters.NewSyncer("beachfront", 0, adapters.ResolveMacros(url), adapters.SyncTypeRedirect)
}
