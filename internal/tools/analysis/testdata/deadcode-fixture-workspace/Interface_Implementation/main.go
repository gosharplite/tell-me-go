package main
import (
	"shared.test/Interface_Implementation/itf"
	"shared.test/Interface_Implementation/impl"
)
func main() {
	var r itf.Runner = impl.MyRunner{}
	r.Run()
}
