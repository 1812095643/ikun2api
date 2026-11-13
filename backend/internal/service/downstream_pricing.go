package service

import "context"

type downstreamPricingKey struct{}

// WithDownstreamPricing is set only from authenticated server-side user data.
func WithDownstreamPricing(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, downstreamPricingKey{}, enabled)
}
func isDownstreamPricing(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(downstreamPricingKey{}).(bool)
	return value
}
func isDownstreamAPIKey(key *APIKey) bool {
	return key != nil && key.User != nil && key.User.IsDownstream
}
func channelPricingApplies(p *ChannelModelPricing, downstream bool) bool {
	return p != nil && (!downstream || p.ApplyToDownstream == nil || *p.ApplyToDownstream)
}
func (s *APIKeyService) UserPricingContext(ctx context.Context, userID int64) (context.Context, error) {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return ctx, err
	}
	return WithDownstreamPricing(ctx, u.IsDownstream), nil
}
func filterSupportedDownstreamPricing(ctx context.Context, models []SupportedModel) {
	if !isDownstreamPricing(ctx) {
		return
	}
	for i := range models {
		if !channelPricingApplies(models[i].Pricing, true) {
			models[i].Pricing = nil
		}
	}
}
