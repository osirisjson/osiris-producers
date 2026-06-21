//go:build bundled

package main

import (
	"go.osirisjson.org/producers/osiris/hyperscalers/aws"
	"go.osirisjson.org/producers/osiris/hyperscalers/azure"
	"go.osirisjson.org/producers/osiris/network/cisco"
)

// builtinRunners maps vendor names to their Run functions when built with -tags bundled.
// PATH-based plugin discovery still works for community/third-party vendors not listed here.
var builtinRunners = map[string]func([]string) error{
	"azure": azure.Run,
	"cisco": cisco.Run,
	"aws": aws.Run,
}
