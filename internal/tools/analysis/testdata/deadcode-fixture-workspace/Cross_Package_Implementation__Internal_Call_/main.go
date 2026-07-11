package main
import (
	"shared.test/Cross_Package_Implementation__Internal_Call_/services"
	"shared.test/Cross_Package_Implementation__Internal_Call_/persistence"
)
func main() {
	svc := services.Service{}
	svc.Do()
	_ = persistence.DB{}
}
