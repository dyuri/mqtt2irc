package bridge

import (
	"fmt"

	"github.com/dyuri/mqtt2irc/pkg/types"
)

// ProcessResult is returned by a Processor after handling a message.
type ProcessResult struct {
	Drop      bool   // if true, discard the message; do not send to IRC
	Formatted string // if non-empty, use this as the IRC message (skips FormatMessage)
}

// Processor is the interface for per-mapping message pre-processors.
type Processor interface {
	Process(msg types.Message) (ProcessResult, error)
}

// ProcessorFactory creates a new Processor from a config map.
type ProcessorFactory func(config map[string]interface{}) (Processor, error)

var processorRegistry = map[string]ProcessorFactory{}

// Register adds a ProcessorFactory to the global registry under the given name.
func Register(name string, factory ProcessorFactory) {
	processorRegistry[name] = factory
}

// NewProcessor instantiates a named processor with the given config.
// Returns an error if the processor name is not registered.
func NewProcessor(name string, config map[string]interface{}) (Processor, error) {
	factory, ok := processorRegistry[name]
	if !ok {
		return nil, fmt.Errorf("unknown processor %q (not registered)", name)
	}
	return factory(config)
}
