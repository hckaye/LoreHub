package httpapi

import statusesapi "github.com/lorehub/lorehub/services/api/internal/statuses"

func WithStatuses(store statusesapi.Store) Option {
	return func(api *API) {
		api.statusesStore = store
	}
}
