package api

// SignalToTopicWeight returns the weight delta to apply to interest_topics
// for a given signal strength (already aggregated: save=2.0, like=1.0, read>=60=0.5).
func SignalToTopicWeight(strength float64) float64 {
	return strength
}

// SignalToTagWeight returns the weight delta for interest_tags. Half of the
// topic weight, so the magnitude stays comparable despite tags being more numerous.
func SignalToTagWeight(strength float64) float64 {
	return strength * 0.5
}

// StrengthFromSignal aggregates a single signal_type+value into the same
// scale used by GetUsersWithStrongSignal (kept here so handler cache-hit
// branches can compute strength without an extra DB round-trip).
func StrengthFromSignal(signalType string, signalValue float64) float64 {
	switch signalType {
	case "save":
		return 2.0
	case "completed_listen":
		return 2.0
	case "like":
		return 1.0
	case "read_duration":
		if signalValue >= 60 {
			return 0.5
		}
		return 0
	}
	return 0
}
