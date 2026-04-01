package household

import "context"

type profileContextKey struct{}
type accessContextKey struct{}

type AccessState struct {
	PinConfigured  bool
	Unlocked       bool
	ExplicitHeader bool
	Protected      bool
}

func WithProfile(ctx context.Context, profile *Profile) context.Context {
	return context.WithValue(ctx, profileContextKey{}, profile)
}

func ProfileFromContext(ctx context.Context) *Profile {
	value := ctx.Value(profileContextKey{})
	profile, _ := value.(*Profile)
	return profile
}

func WithAccessState(ctx context.Context, state AccessState) context.Context {
	return context.WithValue(ctx, accessContextKey{}, state)
}

func AccessStateFromContext(ctx context.Context) AccessState {
	value := ctx.Value(accessContextKey{})
	state, _ := value.(AccessState)
	return state
}
