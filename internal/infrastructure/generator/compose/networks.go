package compose

// NetworkConfig manages network configuration for Docker Compose.
type NetworkConfig struct {
	networks map[string]*Network
}

// NewNetworkConfig creates a new network configuration with default networks.
func NewNetworkConfig() *NetworkConfig {
	nc := &NetworkConfig{
		networks: make(map[string]*Network),
	}

	// Add default web network (public-facing)
	nc.networks["web"] = &Network{
		Driver:   "bridge",
		Internal: false,
		Labels: map[string]string{
			"com.cloudexit.network": "public",
			"com.cloudexit.type":    "web",
		},
	}

	// Add default internal network (private)
	nc.networks["internal"] = &Network{
		Driver:   "bridge",
		Internal: true,
		Labels: map[string]string{
			"com.cloudexit.network": "private",
			"com.cloudexit.type":    "internal",
		},
	}

	return nc
}

// AddNetwork adds a custom network.
func (nc *NetworkConfig) AddNetwork(name string, network *Network) {
	nc.networks[name] = network
}

// GetNetwork retrieves a network by name.
func (nc *NetworkConfig) GetNetwork(name string) (*Network, bool) {
	net, ok := nc.networks[name]
	return net, ok
}

// GetNetworks returns all networks.
func (nc *NetworkConfig) GetNetworks() map[string]*Network {
	return nc.networks
}

// HasNetwork checks if a network exists.
func (nc *NetworkConfig) HasNetwork(name string) bool {
	_, ok := nc.networks[name]
	return ok
}

// NetworkBuilder helps build network configurations.
type NetworkBuilder struct {
	network *Network
}

// NewNetworkBuilder creates a new network builder.
func NewNetworkBuilder(name string) *NetworkBuilder {
	return &NetworkBuilder{
		network: &Network{
			Driver: "bridge",
			Labels: make(map[string]string),
		},
	}
}

// WithDriver sets the network driver.
func (nb *NetworkBuilder) WithDriver(driver string) *NetworkBuilder {
	nb.network.Driver = driver
	return nb
}

// WithInternal marks the network as internal.
func (nb *NetworkBuilder) WithInternal(internal bool) *NetworkBuilder {
	nb.network.Internal = internal
	return nb
}

// WithLabel adds a label to the network.
func (nb *NetworkBuilder) WithLabel(key, value string) *NetworkBuilder {
	nb.network.Labels[key] = value
	return nb
}

// Build returns the built network.
func (nb *NetworkBuilder) Build() *Network {
	return nb.network
}
