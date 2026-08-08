package domain

import "fmt"

// Money values throughout this package are int64 paise (1 INR = 100 paise).
// Never use float64 for money — billing sums many small add-ons over a
// month and float rounding error compounds silently.

// FormatINR renders paise as a "₹123.45" string for display/API responses.
func FormatINR(paise int64) string {
	rupees := paise / 100
	sub := paise % 100
	if sub < 0 {
		sub = -sub
	}
	return fmt.Sprintf("₹%d.%02d", rupees, sub)
}
