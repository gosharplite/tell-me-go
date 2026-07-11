package pkg1
type CustomError interface {
	Error() string
	String() string
	Other()
}
