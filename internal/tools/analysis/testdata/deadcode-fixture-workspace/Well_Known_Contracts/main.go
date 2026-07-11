package main
import (
	"shared.test/Well_Known_Contracts/errors"
	"shared.test/Well_Known_Contracts/strings"
	"shared.test/Well_Known_Contracts/iocontracts"
	"shared.test/Well_Known_Contracts/jsoncontracts"
	"shared.test/Well_Known_Contracts/httpcontracts"
)
func main() {
	_ = errors.APIError{}
	_ = strings.MyStringer{}
	_ = iocontracts.Buffer{}
	_ = jsoncontracts.Payload{}
	_ = httpcontracts.Handler{}
}
