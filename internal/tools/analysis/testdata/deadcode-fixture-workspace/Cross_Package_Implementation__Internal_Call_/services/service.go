package services
type Store interface { Append() }
type Service struct { s Store }
func (svc Service) Do() { svc.s.Append() }
