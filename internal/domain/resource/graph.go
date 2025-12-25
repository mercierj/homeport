package resource

import (
	"fmt"
)

// Graph represents a dependency graph of AWS resources.
// It's used to determine the correct deployment order using topological sorting.
type Graph struct {
	// resources maps resource IDs to their resource objects
	resources map[string]*AWSResource

	// adjacencyList maps each resource ID to the IDs of resources it depends on
	adjacencyList map[string][]string

	// reverseList maps each resource ID to the IDs of resources that depend on it
	reverseList map[string][]string
}

// NewGraph creates a new empty dependency graph.
func NewGraph() *Graph {
	return &Graph{
		resources:     make(map[string]*AWSResource),
		adjacencyList: make(map[string][]string),
		reverseList:   make(map[string][]string),
	}
}

// AddResource adds a resource to the graph.
func (g *Graph) AddResource(resource *AWSResource) {
	g.resources[resource.ID] = resource
	if _, exists := g.adjacencyList[resource.ID]; !exists {
		g.adjacencyList[resource.ID] = make([]string, 0)
	}
	if _, exists := g.reverseList[resource.ID]; !exists {
		g.reverseList[resource.ID] = make([]string, 0)
	}

	// Add all dependencies
	for _, depID := range resource.Dependencies {
		g.AddDependency(resource.ID, depID)
	}
}

// AddDependency adds a dependency relationship between two resources.
// fromID depends on toID (fromID -> toID).
func (g *Graph) AddDependency(fromID, toID string) {
	// Initialize maps if needed
	if _, exists := g.adjacencyList[fromID]; !exists {
		g.adjacencyList[fromID] = make([]string, 0)
	}
	if _, exists := g.reverseList[toID]; !exists {
		g.reverseList[toID] = make([]string, 0)
	}

	// Add the dependency
	g.adjacencyList[fromID] = append(g.adjacencyList[fromID], toID)
	g.reverseList[toID] = append(g.reverseList[toID], fromID)
}

// GetResource retrieves a resource by ID.
func (g *Graph) GetResource(id string) (*AWSResource, bool) {
	res, ok := g.resources[id]
	return res, ok
}

// GetResources returns all resources in the graph.
func (g *Graph) GetResources() []*AWSResource {
	resources := make([]*AWSResource, 0, len(g.resources))
	for _, res := range g.resources {
		resources = append(resources, res)
	}
	return resources
}

// GetDependencies returns the IDs of resources that the given resource depends on.
func (g *Graph) GetDependencies(id string) []string {
	deps, ok := g.adjacencyList[id]
	if !ok {
		return []string{}
	}
	return deps
}

// GetDependents returns the IDs of resources that depend on the given resource.
func (g *Graph) GetDependents(id string) []string {
	deps, ok := g.reverseList[id]
	if !ok {
		return []string{}
	}
	return deps
}

// TopologicalSort performs a topological sort on the dependency graph.
// Returns resources in deployment order (dependencies first).
// Returns an error if a circular dependency is detected.
func (g *Graph) TopologicalSort() ([]*AWSResource, error) {
	// Calculate in-degree (number of dependencies) for each node
	inDegree := make(map[string]int)
	for id := range g.resources {
		inDegree[id] = len(g.adjacencyList[id])
	}

	// Queue of resources with no dependencies
	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Process resources in order
	result := make([]*AWSResource, 0, len(g.resources))
	visited := make(map[string]bool)

	for len(queue) > 0 {
		// Pop from queue
		currentID := queue[0]
		queue = queue[1:]

		// Add to result
		if res, ok := g.resources[currentID]; ok {
			result = append(result, res)
			visited[currentID] = true
		}

		// Process dependents (resources that depend on current)
		for _, depID := range g.reverseList[currentID] {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	// Check for circular dependencies
	if len(result) != len(g.resources) {
		unvisited := make([]string, 0)
		for id := range g.resources {
			if !visited[id] {
				unvisited = append(unvisited, id)
			}
		}
		return nil, fmt.Errorf("circular dependency detected involving resources: %v", unvisited)
	}

	return result, nil
}

// Size returns the number of resources in the graph.
func (g *Graph) Size() int {
	return len(g.resources)
}

// HasCycles checks if the graph contains any circular dependencies.
func (g *Graph) HasCycles() bool {
	_, err := g.TopologicalSort()
	return err != nil
}

// Clone creates a deep copy of the graph.
func (g *Graph) Clone() *Graph {
	clone := NewGraph()

	// Copy resources
	for id, res := range g.resources {
		clone.resources[id] = res
	}

	// Copy adjacency lists
	for id, deps := range g.adjacencyList {
		clone.adjacencyList[id] = make([]string, len(deps))
		copy(clone.adjacencyList[id], deps)
	}

	// Copy reverse lists
	for id, deps := range g.reverseList {
		clone.reverseList[id] = make([]string, len(deps))
		copy(clone.reverseList[id], deps)
	}

	return clone
}
