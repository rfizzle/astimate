// Package greet is the fixture's one package that is analysed.
package greet

// Hello returns a greeting for name.
func Hello(name string) string {
	if name == "" {
		name = "world"
	}
	return "hello, " + name
}
