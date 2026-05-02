package config

// Provider reads the current runtime configuration.
type Provider interface {
	Get() *Config
}

// GlobalProvider adapts the package-level runtime configuration to Provider.
type GlobalProvider struct{}

func (GlobalProvider) Get() *Config {
	cfg := Get()
	if cfg == nil {
		return DefaultConfig()
	}
	return cfg
}

// StaticProvider is useful for services that should use a fixed config snapshot.
type StaticProvider struct {
	Config *Config
}

func (p StaticProvider) Get() *Config {
	if p.Config == nil {
		return DefaultConfig()
	}
	return p.Config
}
