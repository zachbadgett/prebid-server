package adcert

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
)

func NewEndpoint() func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		w.Write([]byte(public))
	}
}
