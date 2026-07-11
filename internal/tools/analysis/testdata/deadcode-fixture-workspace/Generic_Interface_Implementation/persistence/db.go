package persistence
type DB[T any] struct{}
func (db DB[T]) Save(item T) {}
