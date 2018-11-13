package adscert

import "github.com/mxmCherry/openrtb"

type BidRequest struct {
	*openrtb.BidRequest
	PublisherSignature          string `json:"ps,omitempty"`
	PublisherCertificateVersion string `json:"pcv,omitempty"`
}
