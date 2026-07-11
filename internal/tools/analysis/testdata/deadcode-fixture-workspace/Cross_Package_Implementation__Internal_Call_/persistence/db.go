package persistence
import "shared.test/Cross_Package_Implementation__Internal_Call_/services"
type DB struct{}
func (db DB) Append() {}
var _ services.Store = DB{}
