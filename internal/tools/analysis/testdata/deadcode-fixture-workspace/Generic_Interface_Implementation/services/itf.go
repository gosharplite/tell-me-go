package services
type GenericStore[T any] interface { Save(T) }
