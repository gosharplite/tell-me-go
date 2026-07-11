package main
import (
	"shared.test/Generic_Interface_Implementation/services"
	"shared.test/Generic_Interface_Implementation/persistence"
)
func main() {
	var s services.GenericStore[string] = persistence.DB[string]{}
	s.Save("test")
}
