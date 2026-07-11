package pkg1
type InternalItf interface { Run() }
type Impl struct{}
func (i Impl) Run() {}
func Use() { var itf InternalItf = Impl{}; itf.Run() }
