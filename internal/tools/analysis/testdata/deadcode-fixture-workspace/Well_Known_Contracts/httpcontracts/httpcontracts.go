package httpcontracts
import "net/http"
// Handler satisfies http.Handler.
type Handler struct{}
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
