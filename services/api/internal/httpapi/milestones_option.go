package httpapi

import milestonesapi "github.com/lorehub/lorehub/services/api/internal/milestones"

func WithMilestones(store milestonesapi.Store) Option {
	return func(api *API) {
		api.milestonesStore = store
	}
}
