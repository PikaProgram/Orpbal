// Package testsupport holds shared test fixtures and interfaces used by
// generated example tests.
package testsupport


// Hello is an example function exercised by the generated tests.
func Hello(name string) string {
	if name == "" {
		name = "world"
	}
	return "Hello, " + name + "!"
}
