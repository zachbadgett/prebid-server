package rhythmone

import (
	"testing"

	"github.com/prebid/prebid-server/adapters/adapterstest"
)

func TestJsonSamples(t *testing.T) {
	adapterstest.RunJSONBidderTest(t, "rhythmonetest", NewRhythmoneBidder("http://tag.1rx.io/rmp"))
}
