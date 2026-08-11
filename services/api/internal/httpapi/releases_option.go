package httpapi

import releasesapi "github.com/lorehub/lorehub/services/api/internal/releases"

func WithReleases(store releasesapi.Store) Option {
	return func(api *API) {
		api.releasesStore = store
	}
}
