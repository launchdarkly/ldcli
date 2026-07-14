//go:build darwin

package render

// TIOCGWINSZ request code on darwin/BSD.
const tiocgwinsz = 0x40087468
