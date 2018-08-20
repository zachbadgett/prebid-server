package adapters

type registryOption func(entry *registryEntry) error

func WithBidder(b bidderInit) registryOption {
	return func(entry *registryEntry) error {
		entry.bidderInit = b
		return nil
	}
}

func WithAdapter(adapterName string, a adapterInit) registryOption {
	return func(entry *registryEntry) error {
		entry.adapterName = adapterName
		entry.adapterInit = a
		return nil
	}
}

func WithUsersync(s syncerInit) registryOption {
	return func(entry *registryEntry) error {
		entry.syncerInit = s
		return nil
	}
}
