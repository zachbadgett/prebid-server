package endpoints

import (
	"net/http"

	"github.com/golang/glog"
	jsoniter "github.com/json-iterator/go"
)

type versionModel struct {
	Revision string `json:"revision"`
}

// NewVersionEndpoint returns the latest commit sha1 from which the binary was built
func NewVersionEndpoint(version string) func(w http.ResponseWriter, r *http.Request) {
	if version == "" {
		version = "not-set"
	}

	return func(w http.ResponseWriter, r *http.Request) {
		jsonOutput, err := jsoniter.Marshal(versionModel{
			Revision: version,
		})
		if err != nil {
			glog.Errorf("/version Critical error when trying to marshal versionModel: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Write(jsonOutput)
	}
}
