package sonobi

import (
	"net/http"
	"testing"

	"github.com/prebid/prebid-server/adapters/adapterstest"
)

func TestJsonSamples(t *testing.T) {
	sonobiAdapter := NewSonobiBidder(new(http.Client), "https://apex.go.sonobi.com/prebid?partnerid=71d9d3d8af")
	adapterstest.RunJSONBidderTest(t, "sonobitest", sonobiAdapter)
}
