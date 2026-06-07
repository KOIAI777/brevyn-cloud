package admin

import "math"

func isFiniteAmount(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func hasMaxDecimalPlaces(value float64, places int) bool {
	if places < 0 {
		return false
	}
	factor := math.Pow10(places)
	return math.Abs(value*factor-math.Round(value*factor)) < 1e-9
}
