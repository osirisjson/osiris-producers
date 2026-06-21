//go:build !bundled

package main

// builtinRunners is empty in non-bundled builds, all vendors resolved via $PATH.
var builtinRunners = map[string]func([]string) error{}
