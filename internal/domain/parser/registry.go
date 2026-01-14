// Package parser defines the interface for parsing cloud infrastructure configurations.
package parser

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/homeport/homeport/internal/domain/resource"
)

// Registry manages parser implementations.
type Registry struct {
	mu      sync.RWMutex
	parsers map[resource.Provider][]Parser
}

// NewRegistry creates a new parser registry.
func NewRegistry() *Registry {
	return &Registry{
		parsers: make(map[resource.Provider][]Parser),
	}
}

// Register adds a parser to the registry.
func (r *Registry) Register(p Parser) {
	r.mu.Lock()
	defer r.mu.Unlock()

	provider := p.Provider()
	r.parsers[provider] = append(r.parsers[provider], p)
}

// Get returns parsers for a specific provider.
func (r *Registry) Get(provider resource.Provider) []Parser {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.parsers[provider]
}

// GetByFormat returns a parser for a specific provider and format.
func (r *Registry) GetByFormat(provider resource.Provider, format Format) (Parser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parsers, ok := r.parsers[provider]
	if !ok {
		return nil, ErrNoParserFound
	}

	for _, p := range parsers {
		for _, f := range p.SupportedFormats() {
			if f == format {
				return p, nil
			}
		}
	}

	return nil, ErrNoParserFound
}

// All returns all registered parsers.
func (r *Registry) All() []Parser {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []Parser
	for _, parsers := range r.parsers {
		all = append(all, parsers...)
	}
	return all
}

// Providers returns all providers that have registered parsers.
func (r *Registry) Providers() []resource.Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]resource.Provider, 0, len(r.parsers))
	for p := range r.parsers {
		providers = append(providers, p)
	}
	return providers
}

// AutoDetect finds the best parser for the given path.
// It tries all registered parsers and returns the one with highest confidence.
func (r *Registry) AutoDetect(path string) (Parser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type candidate struct {
		parser     Parser
		confidence float64
	}

	var candidates []candidate

	for _, parsers := range r.parsers {
		for _, p := range parsers {
			if canHandle, confidence := p.AutoDetect(path); canHandle {
				candidates = append(candidates, candidate{
					parser:     p,
					confidence: confidence,
				})
			}
		}
	}

	if len(candidates) == 0 {
		return nil, ErrNoParserFound
	}

	// Sort by confidence descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].confidence > candidates[j].confidence
	})

	return candidates[0].parser, nil
}

// AutoDetectAll finds all parsers that can handle the given path.
// Returns parsers sorted by confidence (highest first).
func (r *Registry) AutoDetectAll(path string) []Parser {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type candidate struct {
		parser     Parser
		confidence float64
	}

	var candidates []candidate

	for _, parsers := range r.parsers {
		for _, p := range parsers {
			if canHandle, confidence := p.AutoDetect(path); canHandle {
				candidates = append(candidates, candidate{
					parser:     p,
					confidence: confidence,
				})
			}
		}
	}

	// Sort by confidence descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].confidence > candidates[j].confidence
	})

	result := make([]Parser, len(candidates))
	for i, c := range candidates {
		result[i] = c.parser
	}
	return result
}

// Parse finds the best parser and parses the infrastructure.
func (r *Registry) Parse(ctx context.Context, path string, opts *ParseOptions) (*resource.Infrastructure, error) {
	parser, err := r.AutoDetect(path)
	if err != nil {
		return nil, err
	}

	return parser.Parse(ctx, path, opts)
}

// ParseWithProvider parses using a specific provider's parser.
func (r *Registry) ParseWithProvider(ctx context.Context, path string, provider resource.Provider, opts *ParseOptions) (*resource.Infrastructure, error) {
	parsers := r.Get(provider)
	if len(parsers) == 0 {
		return nil, ErrNoParserFound
	}

	// Find best parser for this provider
	var best Parser
	var bestConf float64

	for _, p := range parsers {
		if canHandle, conf := p.AutoDetect(path); canHandle && conf > bestConf {
			best = p
			bestConf = conf
		}
	}

	if best == nil {
		// Use first parser as fallback
		best = parsers[0]
	}

	return best.Parse(ctx, path, opts)
}

// ParseMulti parses infrastructure that may contain multiple providers.
// It uses auto-detection to find all applicable parsers.
func (r *Registry) ParseMulti(ctx context.Context, path string, opts *ParseOptions) (*resource.Infrastructure, error) {
	parsers := r.AutoDetectAll(path)
	if len(parsers) == 0 {
		return nil, ErrNoParserFound
	}

	// Use the first parser's provider as primary
	var primaryProvider resource.Provider
	if len(parsers) > 0 {
		primaryProvider = parsers[0].Provider()
	}

	// Merge results from all parsers
	result := resource.NewInfrastructure(primaryProvider)

	for _, p := range parsers {
		infra, err := p.Parse(ctx, path, opts)
		if err != nil {
			if opts != nil && opts.IgnoreErrors {
				continue
			}
			return nil, err
		}

		// Merge resources from the parsed infrastructure
		for id, res := range infra.Resources {
			result.Resources[id] = res
		}
	}

	return result, nil
}

// ErrNoParserFound is returned when no suitable parser is found.
var ErrNoParserFound = errors.New("no suitable parser found for the given path")

// Global default registry
var defaultRegistry = NewRegistry()

// DefaultRegistry returns the default parser registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Register adds a parser to the default registry.
func Register(p Parser) {
	defaultRegistry.Register(p)
}

// AutoDetect finds the best parser in the default registry.
func AutoDetect(path string) (Parser, error) {
	return defaultRegistry.AutoDetect(path)
}

// Parse uses the default registry to parse infrastructure.
func Parse(ctx context.Context, path string, opts *ParseOptions) (*resource.Infrastructure, error) {
	return defaultRegistry.Parse(ctx, path, opts)
}
