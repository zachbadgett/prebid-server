package rubicon

import (
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/usersync"
)

func NewRubiconSyncer(cfg *config.Configuration) usersync.Usersyncer {
	usersyncURL := cfg.Adapters[string(BidderRubicon)].UserSyncURL
	return adapters.NewSyncer("rubicon", 52, adapters.ResolveMacros(usersyncURL), adapters.SyncTypeRedirect)
}
