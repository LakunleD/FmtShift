// Package app wires FmtShift's services together. The CLI and the future
// desktop application use it instead of constructing codecs or the engine
// themselves, so neither user interface depends on libvips directly.
package app

import (
	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/codecs/vips"
	"github.com/lakunled/fmtshift/internal/converter"
)

// App holds the services for one process.
type App struct {
	Codec     codecs.Codec
	Converter *converter.Converter
}

// New initialises the image codec and conversion engine.
func New() (*App, error) {
	codec, err := vips.New()
	if err != nil {
		return nil, err
	}
	return &App{Codec: codec, Converter: converter.New(codec)}, nil
}
