package demandforecast

// DataPoint represents a single time-series observation.
type DataPoint struct {
	Period string  // e.g. "2026-01"
	Value  float64
}

// Forecast holds a predicted value for a future period.
type Forecast struct {
	Period    string
	Predicted float64
}

// MovingAverage computes a simple moving average over data with the given window.
// Output length = len(data) - window + 1.
func MovingAverage(data []DataPoint, window int) []DataPoint {
	if window <= 0 || len(data) < window {
		return nil
	}

	out := make([]DataPoint, 0, len(data)-window+1)
	var sum float64
	for i := 0; i < window; i++ {
		sum += data[i].Value
	}
	out = append(out, DataPoint{Period: data[window-1].Period, Value: sum / float64(window)})

	for i := window; i < len(data); i++ {
		sum += data[i].Value - data[i-window].Value
		out = append(out, DataPoint{Period: data[i].Period, Value: sum / float64(window)})
	}
	return out
}

// TrendExtractor extracts linear trends from a data series.
type TrendExtractor struct{}

// LinearTrend performs ordinary least squares on the data series and returns slope and intercept.
// The x-axis is the zero-based index of each DataPoint.
func (TrendExtractor) LinearTrend(data []DataPoint) (slope, intercept float64) {
	n := float64(len(data))
	if n < 2 {
		return 0, 0
	}

	var sumX, sumY, sumXY, sumX2 float64
	for i, dp := range data {
		x := float64(i)
		sumX += x
		sumY += dp.Value
		sumXY += x * dp.Value
		sumX2 += x * x
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, sumY / n
	}

	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n
	return slope, intercept
}

// SeasonalIndex computes the seasonal index using the ratio-to-moving-average method.
// Returns a slice of length seasonLength where each entry is the average ratio
// of actual value to overall mean for that position in the season.
func SeasonalIndex(data []DataPoint, seasonLength int) []float64 {
	if len(data) == 0 || seasonLength <= 0 {
		return nil
	}

	var overall float64
	for _, dp := range data {
		overall += dp.Value
	}
	overall /= float64(len(data))

	if overall == 0 {
		return make([]float64, seasonLength)
	}

	sums := make([]float64, seasonLength)
	counts := make([]int, seasonLength)
	for i, dp := range data {
		pos := i % seasonLength
		sums[pos] += dp.Value / overall
		counts[pos]++
	}

	indices := make([]float64, seasonLength)
	for i := 0; i < seasonLength; i++ {
		if counts[i] > 0 {
			indices[i] = sums[i] / float64(counts[i])
		} else {
			indices[i] = 1.0
		}
	}
	return indices
}

// ProjectNextN forecasts the next n periods using trend plus seasonal index.
func ProjectNextN(data []DataPoint, n int, seasonLength int) []Forecast {
	if len(data) == 0 || n <= 0 {
		return nil
	}

	te := TrendExtractor{}
	slope, intercept := te.LinearTrend(data)
	seasonal := SeasonalIndex(data, seasonLength)

	base := len(data)
	forecasts := make([]Forecast, n)
	for i := 0; i < n; i++ {
		x := float64(base + i)
		trend := intercept + slope*x
		seasonPos := (base + i) % seasonLength
		si := 1.0
		if seasonal != nil && len(seasonal) > seasonPos {
			si = seasonal[seasonPos]
		}
		forecasts[i] = Forecast{
			Period:    labelForIndex(data, base+i),
			Predicted: trend * si,
		}
	}
	return forecasts
}

// labelForIndex generates a period label for the given absolute index.
func labelForIndex(data []DataPoint, absIdx int) string {
	if len(data) == 0 {
		return intToPeriodFallback(absIdx)
	}
	last := data[len(data)-1].Period
	offset := absIdx - (len(data) - 1)
	return shiftPeriod(last, offset)
}

// shiftPeriod adds offset months to a "YYYY-MM" period label.
// Falls back to a numeric label for unrecognised formats.
func shiftPeriod(last string, offset int) string {
	if len(last) == 7 && last[4] == '-' {
		year := int(last[0]-'0')*1000 + int(last[1]-'0')*100 +
			int(last[2]-'0')*10 + int(last[3]-'0')
		month := int(last[5]-'0')*10 + int(last[6]-'0')
		month += offset
		for month > 12 {
			month -= 12
			year++
		}
		for month < 1 {
			month += 12
			year--
		}
		return intToStr4(year) + "-" + intToStr2(month)
	}
	return intToPeriodFallback(offset)
}

func intToPeriodFallback(n int) string {
	s := "period-"
	neg := n < 0
	if neg {
		n = -n
	}
	digits := []byte{}
	if n == 0 {
		digits = append(digits, '0')
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		s += "-"
	}
	return s + string(digits)
}

func intToStr4(n int) string {
	return string([]byte{
		byte('0' + n/1000),
		byte('0' + (n/100)%10),
		byte('0' + (n/10)%10),
		byte('0' + n%10),
	})
}

func intToStr2(n int) string {
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}
