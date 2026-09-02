package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// BasisPoints represents an interest rate or margin in integer basis points (1 bp = 0.01% = 0.0001).
// For example, an APR of 12.45% is stored as 1245 basis points.
type BasisPoints int32

func (bp BasisPoints) ToPercentageString() string {
	val := float64(bp) / 100.0
	return fmt.Sprintf("%.2f%%", val)
}

func (bp BasisPoints) Int32() int32 {
	return int32(bp)
}

// ParsePercentageToBasisPoints parses "12.45%" or "12.45" into 1245 bps
func ParsePercentageToBasisPoints(s string) (BasisPoints, error) {
	cleaned := strings.ReplaceAll(s, "%", "")
	cleaned = strings.TrimSpace(cleaned)
	val, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, err
	}
	return BasisPoints(int32(val*100.0 + 0.5)), nil
}
